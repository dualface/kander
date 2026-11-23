// Package skills contains the agent skills distributed by Kander.
package skills

import "embed"

// Leader contains the kander-leader entry and the references it links to.
//
//go:embed kander-leader
var Leader embed.FS
