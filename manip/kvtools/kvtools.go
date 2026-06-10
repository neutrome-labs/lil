// Package kvtools provides an SDK-style LIL manipulation for caching old tool
// results and exposing a retrieval tool definition.
package kvtools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/neutrome-labs/lil"
	"github.com/neutrome-labs/lil/manip"
)

const (
	// Name is the canonical manip/tool package name.
	Name = "kvtools"

	// DefaultToolName is the injected retrieval tool name.
	DefaultToolName = "get_tool_result"

	// DefaultPrefix is the cache-key prefix.
	DefaultPrefix = "kvtools"

	// DefaultTTL is used for cached tool results.
	DefaultTTL = 30 * time.Minute
)

const defaultDescription = "Retrieve the result of a previous tool call by its ID. Use this when you need data from a tool call that was made earlier in the conversation but whose result is no longer in context."

var defaultSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"tool_call_id": {
			"type": "string",
			"description": "The ID of a previous tool call whose result you want to retrieve."
		}
	},
	"required": ["tool_call_id"]
}`)

type scopeContextKey struct{}

// ContextWithScope returns a context that scopes cache keys for one request,
// trace, conversation, tenant, or other consumer-defined boundary.
func ContextWithScope(ctx context.Context, scope string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, scopeContextKey{}, scope)
}

// ScopeFromContext returns the kvtools scope stored in ctx, if any.
func ScopeFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	scope, _ := ctx.Value(scopeContextKey{}).(string)
	return scope
}

// KeyFunc builds a cache key from prefix, scope, and tool call ID.
type KeyFunc func(prefix, scope, callID string) string

// KVTools caches older completed tool results, strips their RESULT_DATA from
// the LIL program, and injects a get_tool_result tool definition so the model
// can retrieve cached results on demand.
type KVTools struct {
	Store manip.Store

	TTL                    time.Duration
	Prefix                 string
	Scope                  string
	ToolName               string
	ToolDescription        string
	ToolSchema             json.RawMessage
	KeepRecentInteractions int
	InjectToolDef          bool
	IgnoreCacheErrors      bool
	CacheEmptyResults      bool
	KeyFunc                KeyFunc
}

// Option configures KVTools.
type Option = manip.Option[KVTools]

// WithStore sets the cache backend.
func WithStore(store manip.Store) Option {
	return func(k *KVTools) {
		if store != nil {
			k.Store = store
		}
	}
}

// WithTTL sets the TTL for cached tool results. A negative value disables
// expiration for stores that treat ttl <= 0 as no expiration.
func WithTTL(ttl time.Duration) Option {
	return func(k *KVTools) {
		k.TTL = ttl
	}
}

// WithScope sets a static cache scope. ContextWithScope overrides this value
// for context-aware calls.
func WithScope(scope string) Option {
	return func(k *KVTools) {
		k.Scope = scope
	}
}

// WithPrefix sets the cache-key prefix.
func WithPrefix(prefix string) Option {
	return func(k *KVTools) {
		if prefix != "" {
			k.Prefix = prefix
		}
	}
}

// WithToolName sets the injected retrieval tool name.
func WithToolName(name string) Option {
	return func(k *KVTools) {
		if name != "" {
			k.ToolName = name
		}
	}
}

// WithToolDescription sets the injected retrieval tool description.
func WithToolDescription(description string) Option {
	return func(k *KVTools) {
		if description != "" {
			k.ToolDescription = description
		}
	}
}

// WithToolSchema sets the injected retrieval tool JSON schema.
func WithToolSchema(schema json.RawMessage) Option {
	return func(k *KVTools) {
		if len(schema) > 0 {
			k.ToolSchema = cloneRaw(schema)
		}
	}
}

// WithKeepRecentInteractions sets how many most-recent completed tool
// interactions keep their result payloads in the prompt. The default is 1.
func WithKeepRecentInteractions(n int) Option {
	return func(k *KVTools) {
		if n >= 0 {
			k.KeepRecentInteractions = n
		}
	}
}

// WithoutToolDef disables retrieval tool definition injection.
func WithoutToolDef() Option {
	return func(k *KVTools) {
		k.InjectToolDef = false
	}
}

// WithIgnoreCacheErrors controls whether cache Set errors are ignored during
// program manipulation.
func WithIgnoreCacheErrors(ignore bool) Option {
	return func(k *KVTools) {
		k.IgnoreCacheErrors = ignore
	}
}

// WithCacheEmptyResults controls whether empty RESULT_DATA values are cached
// and stripped.
func WithCacheEmptyResults(cache bool) Option {
	return func(k *KVTools) {
		k.CacheEmptyResults = cache
	}
}

// WithKeyFunc customizes cache key construction.
func WithKeyFunc(fn KeyFunc) Option {
	return func(k *KVTools) {
		if fn != nil {
			k.KeyFunc = fn
		}
	}
}

// New creates a KVTools manip with a default in-memory store.
func New(opts ...Option) *KVTools {
	k := &KVTools{
		Store:                  manip.NewMemoryStore(manip.DefaultStoreMaxItems, DefaultTTL),
		TTL:                    DefaultTTL,
		Prefix:                 DefaultPrefix,
		ToolName:               DefaultToolName,
		ToolDescription:        defaultDescription,
		ToolSchema:             cloneRaw(defaultSchema),
		KeepRecentInteractions: 1,
		InjectToolDef:          true,
		KeyFunc:                DefaultKey,
	}
	manip.ApplyOptions(k, opts...)
	return k
}

// DefaultKey builds the default cache key.
func DefaultKey(prefix, scope, callID string) string {
	if prefix == "" {
		prefix = DefaultPrefix
	}
	if scope != "" {
		return prefix + ":" + scope + ":" + callID
	}
	return prefix + "::" + callID
}

// Apply applies the kvtools transform with context.Background.
func (k *KVTools) Apply(prog *lil.Program) (*lil.Program, error) {
	return k.ApplyContext(context.Background(), prog)
}

// ApplyContext caches and strips old tool results, then injects the retrieval
// tool definition when enabled.
func (k *KVTools) ApplyContext(ctx context.Context, prog *lil.Program) (*lil.Program, error) {
	if prog == nil {
		return nil, nil
	}
	if k == nil {
		return prog, nil
	}
	k.withDefaults()

	next, err := k.CacheAndStrip(ctx, prog)
	if err != nil {
		return nil, err
	}
	if k.InjectToolDef {
		next = k.InjectTool(next)
	}
	return next, nil
}

// CacheAndStrip caches RESULT_DATA from older completed tool interactions and
// removes those RESULT_DATA instructions from the returned program.
func (k *KVTools) CacheAndStrip(ctx context.Context, prog *lil.Program) (*lil.Program, error) {
	if prog == nil {
		return nil, nil
	}
	if k == nil {
		return prog, nil
	}
	k.withDefaults()
	if ctx == nil {
		ctx = context.Background()
	}

	msgs := prog.Messages()
	calls := prog.ToolCalls()
	results := prog.ToolResults()
	interactions := completedInteractions(msgs, calls)
	keepRecent := k.KeepRecentInteractions
	if keepRecent < 0 {
		keepRecent = 0
	}
	if len(interactions) <= keepRecent {
		return prog, nil
	}

	toCache := interactions[:len(interactions)-keepRecent]
	scope := k.scope(ctx)
	var removeIndices []int
	for _, interaction := range toCache {
		for msgIndex := interaction.assistIdx + 1; msgIndex <= interaction.endIdx; msgIndex++ {
			if msgs[msgIndex].Role != lil.ROLE_TOOL {
				continue
			}
			for _, result := range results {
				if result.Start < msgs[msgIndex].Start || result.End > msgs[msgIndex].End {
					continue
				}
				indices, data := resultData(prog, result)
				if data == "" && !k.CacheEmptyResults {
					continue
				}
				key := k.KeyFunc(k.Prefix, scope, result.CallID)
				if err := k.Store.Set(ctx, key, data, k.TTL); err != nil && !k.IgnoreCacheErrors {
					return nil, fmt.Errorf("kvtools: cache result %q: %w", result.CallID, err)
				}
				removeIndices = append(removeIndices, indices...)
			}
		}
	}

	if len(removeIndices) == 0 {
		return prog, nil
	}
	sort.Sort(sort.Reverse(sort.IntSlice(removeIndices)))
	next := prog
	for _, idx := range removeIndices {
		next = next.RemoveRange(idx, idx)
	}
	return next, nil
}

// InjectTool returns prog with the retrieval tool definition inserted. If the
// tool definition already exists, prog is returned unchanged.
func (k *KVTools) InjectTool(prog *lil.Program) *lil.Program {
	if prog == nil {
		return nil
	}
	if k == nil {
		return prog
	}
	k.withDefaults()
	if hasToolDef(prog, k.ToolName) {
		return prog
	}
	return injectDefs(prog, k.ToolDef()...)
}

// ToolDef returns the retrieval tool definition instructions.
func (k *KVTools) ToolDef() []lil.Instruction {
	if k == nil {
		k = New()
	}
	k.withDefaults()
	return BuildToolDef(k.ToolName, k.ToolDescription, k.ToolSchema)
}

// Lookup retrieves a cached tool result by original tool call ID.
func (k *KVTools) Lookup(ctx context.Context, toolCallID string) (string, error) {
	if k == nil {
		return "", manip.ErrNotFound
	}
	k.withDefaults()
	if ctx == nil {
		ctx = context.Background()
	}
	return k.Store.Get(ctx, k.KeyFunc(k.Prefix, k.scope(ctx), toolCallID))
}

// HandleToolCall serves a get_tool_result call. handled is false when name does
// not match this KVTools instance's ToolName.
func (k *KVTools) HandleToolCall(ctx context.Context, name string, args json.RawMessage) (result string, handled bool, err error) {
	if k == nil {
		return "", false, nil
	}
	k.withDefaults()
	if name != k.ToolName {
		return "", false, nil
	}

	var input struct {
		ToolCallID string `json:"tool_call_id"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return "invalid arguments: " + err.Error(), true, nil
	}
	if input.ToolCallID == "" {
		return "tool_call_id is required", true, nil
	}

	val, err := k.Lookup(ctx, input.ToolCallID)
	if err != nil {
		return "tool result not found for call_id: " + input.ToolCallID, true, nil
	}
	return val, true, nil
}

// DispatchCalls finds retrieval tool calls in responseProg and returns
// synthetic tool-result messages for handled calls. Apps can append these
// instructions to their next request program in their own inference loop.
func (k *KVTools) DispatchCalls(ctx context.Context, responseProg *lil.Program) ([]lil.Instruction, int, error) {
	if k == nil || responseProg == nil {
		return nil, 0, nil
	}
	k.withDefaults()

	var out []lil.Instruction
	handled := 0
	for _, call := range responseProg.ToolCalls() {
		if call.Name != k.ToolName {
			continue
		}
		args := callArgs(responseProg, call)
		result, ok, err := k.HandleToolCall(ctx, call.Name, args)
		if err != nil {
			return out, handled, err
		}
		if !ok {
			continue
		}
		handled++
		out = append(out, ToolResultMessage(call.CallID, result)...)
	}
	return out, handled, nil
}

// BuildToolDef builds a complete DEF_START..DEF_END instruction sequence.
func BuildToolDef(name, description string, schema json.RawMessage) []lil.Instruction {
	insts := []lil.Instruction{
		{Op: lil.DEF_START},
		{Op: lil.DEF_NAME, Str: name},
		{Op: lil.DEF_DESC, Str: description},
	}
	if len(schema) > 0 {
		insts = append(insts, lil.Instruction{Op: lil.DEF_SCHEMA, JSON: cloneRaw(schema)})
	}
	insts = append(insts, lil.Instruction{Op: lil.DEF_END})
	return insts
}

// ToolResultMessage builds a synthetic tool-result message for callID.
func ToolResultMessage(callID, result string) []lil.Instruction {
	return []lil.Instruction{
		{Op: lil.MSG_START},
		{Op: lil.ROLE_TOOL},
		{Op: lil.RESULT_START, Str: callID},
		{Op: lil.RESULT_DATA, Str: result},
		{Op: lil.RESULT_END},
		{Op: lil.MSG_END},
	}
}

func (k *KVTools) withDefaults() {
	if k.Store == nil {
		k.Store = manip.NewMemoryStore(manip.DefaultStoreMaxItems, DefaultTTL)
	}
	if k.Prefix == "" {
		k.Prefix = DefaultPrefix
	}
	if k.ToolName == "" {
		k.ToolName = DefaultToolName
	}
	if k.ToolDescription == "" {
		k.ToolDescription = defaultDescription
	}
	if len(k.ToolSchema) == 0 {
		k.ToolSchema = cloneRaw(defaultSchema)
	}
	if k.KeyFunc == nil {
		k.KeyFunc = DefaultKey
	}
}

func (k *KVTools) scope(ctx context.Context) string {
	if scope := ScopeFromContext(ctx); scope != "" {
		return scope
	}
	return k.Scope
}

type interaction struct {
	assistIdx int
	endIdx    int
}

func completedInteractions(msgs []lil.MessageSpan, calls []lil.ToolCallSpan) []interaction {
	var out []interaction
	for i := 0; i < len(msgs); i++ {
		if msgs[i].Role != lil.ROLE_AST || !spanHasCalls(calls, msgs[i]) {
			continue
		}
		item := interaction{assistIdx: i, endIdx: i}
		for j := i + 1; j < len(msgs) && msgs[j].Role == lil.ROLE_TOOL; j++ {
			item.endIdx = j
		}
		out = append(out, item)
		i = item.endIdx
	}
	return out
}

func spanHasCalls(calls []lil.ToolCallSpan, span lil.MessageSpan) bool {
	for _, call := range calls {
		if call.Start >= span.Start && call.End <= span.End {
			return true
		}
	}
	return false
}

func resultData(prog *lil.Program, result lil.ToolResultSpan) ([]int, string) {
	var indices []int
	var sb strings.Builder
	for i := result.Start; i <= result.End && i < len(prog.Code); i++ {
		if prog.Code[i].Op == lil.RESULT_DATA {
			indices = append(indices, i)
			sb.WriteString(prog.Code[i].Str)
		}
	}
	return indices, sb.String()
}

func callArgs(prog *lil.Program, call lil.ToolCallSpan) json.RawMessage {
	for i := call.Start; i <= call.End && i < len(prog.Code); i++ {
		if prog.Code[i].Op == lil.CALL_ARGS {
			return cloneRaw(prog.Code[i].JSON)
		}
	}
	return nil
}

func hasToolDef(prog *lil.Program, name string) bool {
	for _, def := range prog.ToolDefs() {
		if def.Name == name {
			return true
		}
	}
	return false
}

func injectDefs(prog *lil.Program, defs ...lil.Instruction) *lil.Program {
	for i := len(prog.Code) - 1; i >= 0; i-- {
		if prog.Code[i].Op == lil.DEF_END {
			return prog.InsertAfter(i, defs...)
		}
	}
	msgs := prog.Messages()
	if len(msgs) > 0 {
		return prog.InsertBefore(msgs[0].Start, defs...)
	}
	next := prog.Clone()
	for _, def := range defs {
		next.Code = append(next.Code, def)
	}
	return next
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	out := make(json.RawMessage, len(raw))
	copy(out, raw)
	return out
}

var (
	_ manip.Manip        = (*KVTools)(nil)
	_ manip.ContextManip = (*KVTools)(nil)
)
