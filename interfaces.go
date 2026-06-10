package lil

// ─── Parser interface ────────────────────────────────────────────────────────

// Parser converts a provider-specific JSON request into an LIL Program.
type Parser interface {
	// ParseRequest converts a raw JSON request body into an LIL program.
	ParseRequest(body []byte) (*Program, error)
}

// Emitter converts an LIL Program into a provider-specific JSON request.
type Emitter interface {
	// EmitRequest converts an LIL program into a raw JSON request body.
	EmitRequest(prog *Program) ([]byte, error)
}

// ResponseParser converts a provider-specific JSON response into an LIL Program.
type ResponseParser interface {
	// ParseResponse converts a raw JSON response body into an LIL program.
	ParseResponse(body []byte) (*Program, error)
}

// ResponseEmitter converts an LIL Program into a provider-specific JSON response.
type ResponseEmitter interface {
	// EmitResponse converts an LIL program into a raw JSON response body.
	EmitResponse(prog *Program) ([]byte, error)
}

// StreamChunkParser converts a provider-specific streaming chunk into LIL instructions.
type StreamChunkParser interface {
	// ParseStreamChunk converts a streaming chunk into an LIL program (partial).
	ParseStreamChunk(body []byte) (*Program, error)
}

// StreamChunkEmitter converts LIL stream instructions into provider-specific chunk JSON.
type StreamChunkEmitter interface {
	// EmitStreamChunk converts an LIL program (partial, from a stream chunk) into JSON.
	EmitStreamChunk(prog *Program) ([]byte, error)
}
