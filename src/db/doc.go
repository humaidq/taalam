/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */

// Package db provides persistence and query helpers.
//
// For the coursework blockchain MVP, this package will own both the normal
// relational LMS state and the append-only blockchain tables. The relational
// tables remain the main application state, while the blockchain tables act as
// an internal hash-chained audit log stored in PostgreSQL and appended
// synchronously by the web server.
package db
