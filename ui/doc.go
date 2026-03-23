// Package ui provides the terminal user interface for ClickHouse Watcher.
//
// The TUI is built using the Bubble Tea framework and consists of multiple views
// that can be navigated using keyboard shortcuts.
//
// # Views
//
// The application has the following views:
//
//   - Connect: Initial connection screen showing ASCII art logo and connection status
//   - Dashboard: System metrics overview (version, uptime, rows, bytes)
//   - Tables: List of tables with their sizes and date ranges
//   - Processes: Currently running queries
//   - History: Historical metrics data (RRD graphs for day/week/month)
//   - Table Detail: Detailed view of a single table with TTL management
//   - Query Executor: SQL query interface with results display
//   - Confirm: Confirmation dialog for destructive actions
//
// # Keyboard Navigation
//
//   - Tab: Cycle through main views (Dashboard → Tables → Processes → History → Query → Dashboard)
//   - ↑/↓: Navigate in Tables view
//   - ←/→: Change history period in History view
//   - Enter: Select table details, execute query, confirm action
//   - Esc: Go back / quit
//   - R: Refresh current view
//   - H: Open history from dashboard
//   - T: Truncate table (in table detail)
//   - L: Apply TTL (in table detail)
//
// # Architecture
//
// The ui.Model struct is the main application state. It implements tea.Model
// interface with Init(), Update(), and View() methods. The Update method dispatches
// to specific handler functions based on the current view state.
//
// Data is fetched from the daemon via the client package using a Unix socket connection.
// The connect() command runs asynchronously and updates the model state when complete.
package ui
