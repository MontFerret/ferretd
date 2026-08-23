package dap

import (
	protocol "github.com/google/go-dap"

	"github.com/MontFerret/ferretd/internal/logging"
)

type (
	logField         string
	logDirection     string
	logKind          string
	logComponent     string
	sessionEndStatus string
	stopReason       string
	outputCategory   string
	logEnricher      func(logging.Record)
)

const (
	logFieldArgumentsBytes     logField = "arguments_bytes"
	logFieldBytes              logField = "bytes"
	logFieldCategory           logField = "category"
	logFieldColumn             logField = "column"
	logFieldCommand            logField = "command"
	logFieldComponent          logField = "component"
	logFieldContext            logField = "context"
	logFieldCount              logField = "count"
	logFieldCWD                logField = "cwd"
	logFieldDebugSessionID     logField = "debug_session_id"
	logFieldDirection          logField = "direction"
	logFieldError              logField = "error"
	logFieldErrorType          logField = "error_type"
	logFieldEvent              logField = "event"
	logFieldEventSequence      logField = "event_seq"
	logFieldExecutionSessionID logField = "execution_session_id"
	logFieldExitCode           logField = "exit_code"
	logFieldExpressionLength   logField = "expression_length"
	logFieldFrameID            logField = "frame_id"
	logFieldFrameIndex         logField = "frame_index"
	logFieldFrames             logField = "frames"
	logFieldHitBreakpoints     logField = "hit_breakpoints"
	logFieldKind               logField = "kind"
	logFieldLaunchProgram      logField = "launch_program"
	logFieldLevels             logField = "levels"
	logFieldLine               logField = "line"
	logFieldPathFormat         logField = "path_format"
	logFieldProgram            logField = "program"
	logFieldReason             logField = "reason"
	logFieldRequestSequence    logField = "request_seq"
	logFieldResponseSequence   logField = "response_seq"
	logFieldRestart            logField = "restart"
	logFieldRoot               logField = "root"
	logFieldScopes             logField = "scopes"
	logFieldSource             logField = "source"
	logFieldSourceReference    logField = "source_reference"
	logFieldStart              logField = "start"
	logFieldStartFrame         logField = "start_frame"
	logFieldStatus             logField = "status"
	logFieldStopOnEntry        logField = "stop_on_entry"
	logFieldSuccess            logField = "success"
	logFieldSuppressed         logField = "suppressed"
	logFieldSuspendDebuggee    logField = "suspend_debuggee"
	logFieldThreadID           logField = "thread_id"
	logFieldTotalFrames        logField = "total_frames"
	logFieldVariables          logField = "variables"
	logFieldVariablesReference logField = "variables_reference"
	logFieldWorkspaceID        logField = "workspace_id"
)

const (
	logDirectionInbound  logDirection = "<-"
	logDirectionOutbound logDirection = "->"
)

const (
	logKindRequest  logKind = "request"
	logKindResponse logKind = "response"
	logKindEvent    logKind = "event"
)

const logComponentDAP logComponent = "dap"

const (
	eventInitialized = "initialized"
	eventStopped     = "stopped"
	eventOutput      = "output"
	eventExited      = "exited"
	eventTerminated  = "terminated"
)

const (
	sessionEndCompleted sessionEndStatus = "completed"
	sessionEndCanceled  sessionEndStatus = "canceled"
	sessionEndFailed    sessionEndStatus = "failed"
)

const (
	stopReasonEntry      stopReason = "entry"
	stopReasonBreakpoint stopReason = "breakpoint"
	stopReasonStep       stopReason = "step"
	stopReasonPause      stopReason = "pause"
	stopReasonException  stopReason = "exception"
)

const (
	outputCategoryStdout outputCategory = "stdout"
	outputCategoryStderr outputCategory = "stderr"
)

const (
	logMessageConfigurationCompleted    = "DAP configuration completed"
	logMessageContinueAfterEntryFailed  = "DAP continue after entry failed"
	logMessageDebugSessionCleanupFailed = "DAP debug session cleanup termination failed"
	logMessageDebugSessionCreated       = "DAP debug session created"
	logMessageDebugSessionWatchFailed   = "DAP debug session watch failed"
	logMessageEvent                     = "DAP event"
	logMessageExecutionCompleted        = "DAP execution completed"
	logMessageExecutionFailed           = "DAP execution failed"
	logMessageExecutionRunning          = "DAP execution running"
	logMessageExecutionStopped          = "DAP execution stopped"
	logMessageExecutionTerminated       = "DAP execution terminated"
	logMessageRequest                   = "DAP request"
	logMessageRequestFailed             = "DAP request failed"
	logMessageResponse                  = "DAP response"
	logMessageSessionEnded              = "DAP session ended"
	logMessageSessionStarted            = "DAP session started"
	logMessageSendExitedEventFailed     = "send DAP exited event failed"
	logMessageSendOutputEventFailed     = "send DAP output event failed"
	logMessageSendStoppedEventFailed    = "send DAP stopped event failed"
	logMessageSendTerminatedEventFailed = "send DAP terminated event failed"
)

func (f logField) FieldName() string {
	return string(f)
}

func (d logDirection) String() string {
	return string(d)
}

func (k logKind) String() string {
	return string(k)
}

func (c logComponent) String() string {
	return string(c)
}

func (s sessionEndStatus) String() string {
	return string(s)
}

func (r stopReason) String() string {
	return string(r)
}

func (c outputCategory) String() string {
	return string(c)
}

func (s *Server) traceRequest(request *protocol.Request, enrichers ...logEnricher) {
	record := s.logger.Debug().
		Enum(logFieldDirection, logDirectionInbound).
		Enum(logFieldKind, logKindRequest).
		String(logFieldCommand, request.Command).
		Int(logFieldRequestSequence, request.Seq)

	for _, enrich := range enrichers {
		enrich(record)
	}

	record.Msg(logMessageRequest)
}

func (s *Server) sendResponse(
	request *protocol.Request,
	build func(protocol.ProtocolMessage) protocol.Message,
	enrichers ...logEnricher,
) error {
	return s.send(build, func(sequence int) {
		record := s.logger.Debug().
			Enum(logFieldDirection, logDirectionOutbound).
			Enum(logFieldKind, logKindResponse).
			String(logFieldCommand, request.Command).
			Int(logFieldRequestSequence, request.Seq).
			Int(logFieldResponseSequence, sequence).
			Bool(logFieldSuccess, true)

		for _, enrich := range enrichers {
			enrich(record)
		}

		record.Msg(logMessageResponse)
	})
}

func (s *Server) sendEvent(
	event string,
	build func(protocol.ProtocolMessage) protocol.Message,
	enrichers ...logEnricher,
) error {
	return s.send(build, func(sequence int) {
		record := s.logger.Debug().
			Enum(logFieldDirection, logDirectionOutbound).
			Enum(logFieldKind, logKindEvent).
			String(logFieldEvent, event).
			Int(logFieldEventSequence, sequence)

		for _, enrich := range enrichers {
			enrich(record)
		}

		record.Msg(logMessageEvent)
	})
}

func (s *Server) logRequestFailure(request *protocol.Request, err error, enrichers ...logEnricher) {
	record := s.logger.Warn().
		Enum(logFieldDirection, logDirectionOutbound).
		Enum(logFieldKind, logKindResponse).
		String(logFieldCommand, request.Command).
		Int(logFieldRequestSequence, request.Seq).
		Bool(logFieldSuccess, false).
		Err(err)

	for _, enrich := range enrichers {
		enrich(record)
	}

	record.Msg(logMessageRequestFailed)
}

func (s *Server) attachSessionLogger(owned ownedSession) {
	s.logger = s.logger.With().
		String(logFieldWorkspaceID, owned.workspace.String()).
		String(logFieldExecutionSessionID, owned.session.String()).
		String(logFieldDebugSessionID, owned.debug.String()).
		Logger()
}
