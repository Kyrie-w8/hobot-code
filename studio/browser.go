package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func openBrowserURL(ctx context.Context, target string) {
	runtime.BrowserOpenURL(ctx, target)
}
