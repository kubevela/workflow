# Package Usage

## Context
First, this context is compatible with built-in context interface.
Also it supports fork and commit like trace span.

### Fork
`Fork` will generate a sub context that inherit the parent's tags. When new tags are added to the `sub-context`, the `parent-context` will not be affected.

### Commit
`Commit` will log the context duration, and export metrics or other execution information.

### usage
```
tracerCtx:=context.NewTraceContext(stdCtx,"$id") 
defer tracerCtx.Commit("success")

// Execute sub-code logic
subCtx:=tracerCtx.Fork("sub-id")
...
subCtx.Commit("step is executed")

```
