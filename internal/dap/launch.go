package dap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	protocol "github.com/google/go-dap"
	"github.com/rs/zerolog"

	"github.com/MontFerret/ferretd/internal/debug"
	"github.com/MontFerret/ferretd/internal/exec"
)

func (s *Server) handleLaunch(ctx context.Context, request *protocol.LaunchRequest, attempt *launchAttempt) error {
	s.traceRequest(request.GetRequest(), func(event *zerolog.Event) {
		event.Int("arguments_bytes", len(request.Arguments))
	})
	s.stateMu.Lock()
	delete(s.queuedLaunches, attempt)

	if !s.initialized || s.launched || s.pendingLaunch != nil {
		s.stateMu.Unlock()

		return s.sendFailure(request.GetRequest(), errors.New("launch requires one successful initialize"))
	}

	attempt.admitted = true
	s.pendingLaunch = attempt
	s.currentLaunch = attempt
	s.stateMu.Unlock()

	var arguments launchArguments
	if err := json.Unmarshal(request.Arguments, &arguments); err != nil {
		return s.rejectLaunch(attempt, fmt.Errorf("invalid launch arguments: %w", err))
	}

	runtimeName, err := arguments.runtimeName()
	if err != nil {
		return s.rejectLaunch(attempt, err)
	}

	if err := arguments.validateDebugMode(); err != nil {
		return s.rejectLaunch(attempt, err)
	}

	diagnosticName := runtimeName
	if diagnosticName == "" {
		diagnosticName = s.catalog.DefaultName()
	}

	s.stateMu.Lock()
	attempt.runtimeName = diagnosticName
	s.stateMu.Unlock()
	diagnosticProgram, diagnosticRoot := arguments.Program, arguments.CWD
	fields := func(event *zerolog.Event) {
		event.Str("runtime_name", diagnosticName).Str("program", diagnosticProgram).Str("root", diagnosticRoot)
		arguments.logWorkingDirectory(event)
	}

	paths, err := arguments.resolvePaths()
	if err != nil {
		return s.rejectLaunch(attempt, err, fields)
	}

	diagnosticProgram, diagnosticRoot = paths.program, paths.root

	options, err := arguments.runtimeOptions()
	if err != nil {
		return s.rejectLaunch(attempt, err, fields)
	}

	owned, watch, err := s.prepareLaunch(ctx, arguments, paths, options, runtimeName)
	if err != nil {
		return s.rejectLaunch(attempt, err, fields, owned.logIDs)
	}

	s.stateMu.Lock()
	err = errors.Join(ctx.Err(), attempt.cause)

	if s.closing {
		err = errors.Join(err, errAdapterClosing)
	}

	if err != nil {
		s.stateMu.Unlock()
		cleanupErr := s.closeLaunchResources(owned, watch)
		s.retainRollback(cleanupErr)

		return s.rejectLaunch(attempt, errors.Join(err, cleanupErr), fields)
	}

	s.ownedClosed = false
	s.owned = owned
	s.watch = watch
	s.launched = true
	s.watchStop = make(chan struct{})
	s.watchDone = make(chan struct{})
	attempt.runtimeName = owned.runtimeName
	s.attachSessionLogger(owned)
	s.stateMu.Unlock()

	event := s.logger.Info().Str("program", paths.program).Str("root", paths.root).Bool("stop_on_entry", arguments.StopOnEntry)

	if directory := owned.workingDirectory; directory != "" {
		event.Str("working_directory", directory)
	}

	event.Msg("DAP debug session created")
	s.eventMu.Lock()
	s.initialStop = &initialStop{}
	s.eventMu.Unlock()

	// The worker exists even when initialized cannot be written, so teardown
	// has exactly one watch completion to join on every published path.
	go s.watchDebugSession(watch)

	return s.sendEvent(eventInitialized, func(base protocol.ProtocolMessage) protocol.Message {
		return &protocol.InitializedEvent{Event: protocol.Event{ProtocolMessage: base, Event: eventInitialized}}
	})
}

func (s *Server) prepareLaunch(
	ctx context.Context,
	arguments launchArguments,
	paths launchPaths,
	options exec.RuntimeOptions,
	runtimeName string,
) (owned ownedSession, watch debug.Subscription, result error) {
	defer func() {
		if result != nil {
			cleanupErr := s.closeLaunchResources(owned, watch)
			s.retainRollback(errors.Join(cleanupErr, preparationCleanup(result)))
			result = errors.Join(result, cleanupErr)
		}
	}()

	opened, err := s.workspaces.Open(ctx, paths.root)
	if err != nil {
		return owned, watch, err
	}

	owned.workspace = opened.ID()

	session, err := s.executions.CreateSession(ctx, opened.ID(), paths.relativePath, runtimeName)
	if err != nil {
		return owned, watch, err
	}

	owned.session = session.ID
	owned.runtimeName = session.RuntimeName

	created, err := s.debugs.CreateSession(ctx, session.ID, exec.Parameters(arguments.Parameters), options)
	if err != nil {
		return owned, watch, err
	}

	owned.debug = created.ID
	owned.workingDirectory = created.Options.WorkingDirectory

	watch, err = s.debugs.WatchSession(ctx, created.ID)
	if err != nil {
		return owned, watch, err
	}

	owned.root = paths.root
	owned.program = paths.program
	owned.programIdentity = paths.identity
	owned.stopOnEntry = arguments.StopOnEntry
	owned.coordinates = newSourceCoordinates(session.Text, s.client)

	return owned, watch, nil
}

func (s *Server) rejectLaunch(attempt *launchAttempt, cause error, fields ...logEnricher) error {
	s.stateMu.Lock()
	cancelled := attempt.cause
	name := attempt.runtimeName

	if s.pendingLaunch == attempt {
		s.pendingLaunch = nil
	}

	if !s.launched && s.currentLaunch == attempt {
		s.currentLaunch = nil
	}

	closing := s.closing
	s.stateMu.Unlock()

	if closing {
		return nil
	}

	detail := errors.Join(cause, cancelled)

	if name != "" {
		detail = fmt.Errorf("runtime %q: %w", name, detail)
	}

	message := detail

	if errors.Is(cancelled, errLaunchCancelled) {
		message = errLaunchCancelled
	}

	return s.sendFailureDetails(&attempt.request, message, detail, detail, fields...)
}

func (s *Server) resolvePendingLaunch() error {
	// send's writer lock orders the outcome commitment immediately before its
	// framed write. Cancellation never needs that writer lock.
	var committed *protocol.Request

	return s.send(func(base protocol.ProtocolMessage) protocol.Message {
		s.stateMu.Lock()
		defer s.stateMu.Unlock()

		attempt := s.pendingLaunch
		if attempt == nil || attempt.cause != nil || s.closing {
			return nil
		}

		committed = &attempt.request
		s.pendingLaunch = nil
		response := s.response(&attempt.request)
		response.ProtocolMessage = base

		return &launchResponse{LaunchResponse: protocol.LaunchResponse{Response: response}, Body: launchResponseBody{RuntimeName: s.owned.runtimeName}}
	}, func(sequence int) {
		if committed != nil {
			s.logResponse(committed, sequence)
		}
	})
}

func (s *Server) failPendingLaunch(err error) error {
	s.stateMu.Lock()
	attempt := s.pendingLaunch
	s.stateMu.Unlock()

	if attempt == nil {
		return nil
	}

	return s.rejectLaunch(attempt, err)
}

func (s *Server) reconcileLaunch() error {
	s.stateMu.Lock()
	attempt := s.pendingLaunch
	watchFailure := s.watchFailure
	cancelled := attempt != nil && attempt.cause != nil && !errors.Is(attempt.cause, errLaunchTerminated) && !errors.Is(attempt.cause, errLaunchDisconnected)
	launched := s.launched
	s.stateMu.Unlock()

	if watchFailure != nil {
		cleanupErr := s.closeOwnedLaunch()
		if err := s.failPendingLaunch(errors.Join(watchFailure, cleanupErr)); err != nil {
			return err
		}

		s.eventMu.Lock()
		result := s.sendOutput(outputCategoryStderr, watchFailure.Error()+"\n")
		result = errors.Join(result, s.sendTerminated())
		s.eventMu.Unlock()

		return errors.Join(watchFailure, result)
	}

	if !cancelled {
		return nil
	}

	cleanupErr := s.closeOwnedLaunch()
	if err := s.rejectLaunch(attempt, cleanupErr); err != nil {
		return err
	}

	if launched {
		s.eventMu.Lock()
		defer s.eventMu.Unlock()

		return s.sendTerminated()
	}

	return nil
}
