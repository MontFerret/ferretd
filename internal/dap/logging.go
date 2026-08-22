package dap

import (
	protocol "github.com/google/go-dap"
	"github.com/rs/zerolog"
)

type logEnricher func(*zerolog.Event)

func (s *Server) traceRequest(request *protocol.Request, enrichers ...logEnricher) {
	event := s.logger.Debug().
		Str("direction", "<-").
		Str("kind", "request").
		Str("command", request.Command).
		Int("request_seq", request.Seq)

	for _, enrich := range enrichers {
		enrich(event)
	}

	event.Msg("DAP request")
}

func (s *Server) sendResponse(
	request *protocol.Request,
	build func(protocol.ProtocolMessage) protocol.Message,
	enrichers ...logEnricher,
) error {
	return s.send(build, func(sequence int) {
		event := s.logger.Debug().
			Str("direction", "->").
			Str("kind", "response").
			Str("command", request.Command).
			Int("request_seq", request.Seq).
			Int("response_seq", sequence).
			Bool("success", true)

		for _, enrich := range enrichers {
			enrich(event)
		}

		event.Msg("DAP response")
	})
}

func (s *Server) sendEvent(
	event string,
	build func(protocol.ProtocolMessage) protocol.Message,
	enrichers ...logEnricher,
) error {
	return s.send(build, func(sequence int) {
		record := s.logger.Debug().
			Str("direction", "->").
			Str("kind", "event").
			Str("event", event).
			Int("event_seq", sequence)

		for _, enrich := range enrichers {
			enrich(record)
		}

		record.Msg("DAP event")
	})
}

func (s *Server) logRequestFailure(request *protocol.Request, err error, enrichers ...logEnricher) {
	event := s.logger.Warn().
		Str("direction", "->").
		Str("kind", "response").
		Str("command", request.Command).
		Int("request_seq", request.Seq).
		Bool("success", false).
		Err(err)

	for _, enrich := range enrichers {
		enrich(event)
	}

	event.Msg("DAP request failed")
}

func (s *Server) attachSessionLogger(owned ownedSession) {
	s.logger = s.logger.With().
		Str("workspace_id", owned.workspace.String()).
		Str("execution_session_id", owned.session.String()).
		Str("debug_session_id", owned.debug.String()).
		Logger()
}
