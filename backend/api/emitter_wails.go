package api

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// WailsEmitter forwards binding events to the Wails runtime so the Svelte frontend
// receives them via EventsOn. It holds the application context captured at startup.
// This is the only file in the api package that depends on the Wails runtime, which
// keeps the bindings and their tests runtime-independent.
type WailsEmitter struct {
	ctx context.Context
}

// NewWailsEmitter builds an emitter bound to the app context provided by Wails in
// the OnStartup lifecycle hook.
func NewWailsEmitter(ctx context.Context) *WailsEmitter {
	return &WailsEmitter{ctx: ctx}
}

// Emit publishes data on the named channel to all frontend listeners.
func (w *WailsEmitter) Emit(channel string, data interface{}) {
	runtime.EventsEmit(w.ctx, channel, data)
}
