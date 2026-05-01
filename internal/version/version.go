// =============================================================================
// File: internal/version/version.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Package version exposes SpiceEdit's release version. Keep this file
// tiny — it's the single source of truth that release CI will bump,
// and a one-line diff is trivial to review.
package version

// Version is the SpiceEdit release version, displayed in the menu footer.
// Bump this constant on each release (or let release automation do it).
const Version = "0.0.22"
