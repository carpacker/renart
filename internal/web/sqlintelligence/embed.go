//go:build renart_sqlglot_fallback

package sqlintelligence

import "embed"

//go:embed python/*
var pythonSource embed.FS
