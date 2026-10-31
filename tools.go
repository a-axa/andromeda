//go:build tools

package main

// Pins TUI dependencies until ui/ imports them directly (Fase 4).
import (
	_ "github.com/charmbracelet/bubbletea"
	_ "github.com/charmbracelet/lipgloss"
)
