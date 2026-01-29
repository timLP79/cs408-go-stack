# Go Full-Stack Web Application

A simple full-stack web application built with Go, Gin web framework, and SQLite database.

**CS408 Spring 2026 Project** | [GitHub Issues](https://github.com/timLP79/cs408-go-stack/issues) | [Project Board](https://github.com/timLP79/cs408-go-stack/projects)

## Project Status

**Current Sprint:** Sprint 1 - Foundation (Week 1-2)

**Completed Milestones:**
- ✅ Milestone 1: Hello World App ([Issue #1](https://github.com/timLP79/cs408-go-stack/issues/1))
  - Basic Gin web server setup
  - Template rendering with layout pattern
  - Bootstrap CDN integration
  - Clean project structure

**Next Up:**
- Issue #2: Add static file serving (Priority: High)
- Issue #8: Set up testing infrastructure (Priority: High)

**Overall Progress:** 1/11 issues completed

### What I've Learned So Far

**Milestone 1 Accomplishments:**
- ✅ Set up Go module with `go.mod` and dependency management
- ✅ Configured Gin web framework for HTTP routing
- ✅ Implemented Go template rendering with layout pattern
- ✅ Integrated Bootstrap 5 via CDN for styling
- ✅ Created clean project structure following Go conventions
- ✅ Learned Git workflow with issue tracking
- ✅ Used `Closes #N` syntax to auto-close GitHub issues

**Key Go Concepts Mastered:**
- Package structure and imports
- Gin router setup (`gin.Default()`, `router.GET()`)
- Template execution (`ExecuteTemplate()`)
- Go template syntax (`{{define}}`, `{{block}}`, `{{.Variable}}`)
- Struct types and data passing to templates
- Environment configuration with `os.Getenv()`

**Development Tools:**
- `go run .` - Run without building
- `go build` - Compile to executable
- `go mod download` - Install dependencies
- Git commit messages with issue references

## Sprint Plan

### Sprint 1: Foundation (Week 1-2) 🔵 CURRENT
**Focus:** Get basic web app working with styling
- [x] Issue #1: ✅ Hello World App (COMPLETE)
- [ ] Issue #2: Add static file serving (Priority: High)
- [ ] Issue #3: Enhance templates with Bootstrap styling
- [ ] Issue #7: Add error page template
- [ ] Issue #8: Set up testing infrastructure (Priority: High)

### Sprint 2: Database (Week 3)
**Focus:** Add data persistence
- [ ] Issue #4: Add database integration (SQLite)
- [ ] Issue #9: Write unit tests for database layer

### Sprint 3: API & Handlers (Week 4)
**Focus:** Build the API
- [ ] Issue #5: Create HTTP handlers for todo routes
- [ ] Issue #10: Write integration tests for HTTP handlers

### Sprint 4: UI & Polish (Week 5)
**Focus:** Complete the application
- [ ] Issue #6: Create todo list UI page
- [ ] Issue #11: Add end-to-end testing (Optional)

## GitHub Labels

Issues are organized with these labels:
- `testing` - Test-related work
- `database` - Database work
- `frontend` - UI/templates
- `backend` - Server-side logic
- `priority-high` - Important tasks
- `priority-low` - Can wait
- `learning` - Educational value
- `blocked` - Waiting on dependencies
- `milestone` - Major deliverables

## Tech Stack

- **Language**: Go 1.22+
- **Web Framework**: [Gin](https://github.com/gin-gonic/gin)
- **Database**: SQLite (via [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) - pure Go, no CGo)
- **Templating**: Go `html/template` with layout pattern
- **CSS**: Bootstrap 5 (CDN)

## Project Structure

```
go-full-stack/
├── main.go              # Entry point: router, templates, server
├── handlers.go          # HTTP handler functions (future)
├── db.go                # Database manager (future)
├── templates/
│   ├── layout.html      # Base layout template
│   ├── index.html       # Landing page content
│   └── error.html       # Error page content
├── static/
│   ├── stylesheets/     # CSS files
│   ├── javascripts/     # JS files
│   └── images/          # Image assets
├── data/                # SQLite database (gitignored)
├── go.mod               # Go module definition
├── go.sum               # Dependency checksums
├── GO_LEARNING_GUIDE.md # Learning reference
└── README.md            # This file
```

## Getting Started

### Prerequisites

- Go 1.22 or higher
- Git

### Installation

1. Clone the repository:
   ```bash
   git clone <your-repo-url>
   cd go-full-stack
   ```

2. Install dependencies:
   ```bash
   go mod download
   ```

3. Build the application:
   ```bash
   go build -o go-full-stack .
   ```

4. Run the application:
   ```bash
   ./go-full-stack
   ```

5. Visit `http://localhost:3000` in your browser

### Development

To run without building:
```bash
go run .
```

To run with live reload, use [air](https://github.com/cosmtrek/air):
```bash
go install github.com/cosmtrek/air@latest
air
```

## Configuration

The application uses environment variables for configuration:

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `3000` | HTTP server port |
| `DATA_DIR` | `data` | Directory for SQLite database |
| `DB_NAME` | `database.sqlite` | Database filename |
| `GO_ENV` | (none) | Set to `test` to enable test utilities |

Example:
```bash
PORT=8080 DATA_DIR=/var/data ./go-full-stack
```

## Database

The application uses SQLite with the following schema:

### `todos` table

| Column | Type | Constraints |
|--------|------|-------------|
| id | INTEGER | PRIMARY KEY AUTOINCREMENT |
| task | TEXT | NOT NULL |
| completed | INTEGER | DEFAULT 0 (boolean: 0 or 1) |

### Database Methods (when implemented)

- `GetAllTodos()` - Retrieve all todos
- `GetTodoByID(id)` - Retrieve a specific todo
- `CreateTodo(task)` - Create a new todo
- `UpdateTodo(id, task, completed)` - Update a todo
- `DeleteTodo(id)` - Delete a todo
- `ToggleTodo(id)` - Toggle completion status
- `GetTotalTodos()` - Count total todos
- `GetCompletedTodos()` - Count completed todos
- `ClearDatabase()` - Delete all todos (test mode only)
- `SeedTestData()` - Insert test data (test mode only)

## Routes

| Method | Path | Description |
|--------|------|-------------|
| GET | `/` | Landing page (Hello World) |
| GET | `/favicon.svg` | Favicon (when static assets added) |
| GET | `/stylesheets/*` | CSS files (when static assets added) |
| GET | `/javascripts/*` | JavaScript files (when static assets added) |
| GET | `/images/*` | Image files (when static assets added) |

More routes will be added as the application grows.

## Templates

The application uses Go's `html/template` package with a layout pattern:

- **`layout.html`** - Base template with HTML structure, Bootstrap CDN, header, footer
- **`index.html`** - Defines `{{define "content"}}` block for the landing page
- **`error.html`** - Defines `{{define "content"}}` block for error pages

### Template Syntax

Go templates use `{{ }}` for expressions:

| Syntax | Description |
|--------|-------------|
| `{{.Title}}` | Output a variable |
| `{{define "name"}}...{{end}}` | Define a template block |
| `{{block "name" .}}{{end}}` | Insert a block (with fallback) |
| `{{template "name" .}}` | Include another template |

## Learning Resources

- [GO_LEARNING_GUIDE.md](./GO_LEARNING_GUIDE.md) - Go syntax guide with examples from this project
- [Gin Documentation](https://gin-gonic.com/docs/)
- [Go Templates Documentation](https://pkg.go.dev/html/template)
- [Tour of Go](https://go.dev/tour/)

## Development Workflow

### Agile/Scrum Process

This project follows Agile/Scrum methodology:

1. **Sprint Planning**: Work is organized into 2-week sprints
2. **GitHub Issues**: Each feature/task has a labeled issue
3. **Git Workflow**: Commits reference and close issues
4. **Testing**: TDD approach with unit, integration, and E2E tests

### Git Workflow with Issue Tracking

1. Pick an issue from the [GitHub Issues board](https://github.com/timLP79/cs408-go-stack/issues)
2. Make your changes to `.go` files or templates
3. Test locally: `go run .` and visit `http://localhost:3000`
4. Stage and commit with issue reference:
   ```bash
   git add <files>
   git commit -m "Description of changes

   Closes #<issue-number>"
   ```
5. Push to GitHub:
   ```bash
   git push origin main
   ```
6. Issue automatically closes and links to your commit

### Local Development

**Quick run (no build):**
```bash
go run .
```

**Build and run:**
```bash
go build -o go-full-stack .
./go-full-stack
```

**Live reload with air:**
```bash
go install github.com/cosmtrek/air@latest
air
```

### Example Commit Message

```
Clean up layout.html template

Remove duplicate content and instructional text from layout.html:
- Removed extra {{define "content"}} block (belongs in index.html)
- Removed "Step 4: Create templates/index.html" instruction text
- Layout template now properly ends at line 12

This completes the Hello World milestone with clean template structure.

Closes #1
```

## Testing

This project follows Test-Driven Development (TDD) practices.

### Testing Strategy

**Test Pyramid:**
1. **Unit Tests** ([Issue #9](https://github.com/timLP79/cs408-go-stack/issues/9))
   - Test individual functions in isolation
   - Database layer tests
   - Files: `db_test.go`, `handlers_test.go`

2. **Integration Tests** ([Issue #10](https://github.com/timLP79/cs408-go-stack/issues/10))
   - Test HTTP handlers with real requests
   - Test database interactions
   - Files: `main_test.go`

3. **End-to-End Tests** ([Issue #11](https://github.com/timLP79/cs408-go-stack/issues/11))
   - Test complete user workflows
   - Optional: Browser automation

### Running Tests

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run with coverage report
go test -cover ./...

# Run specific test file
go test -v db_test.go db.go
```

### Test File Naming Convention

Go automatically finds test files:
- `main.go` → tests in `main_test.go`
- `db.go` → tests in `db_test.go`
- `handlers.go` → tests in `handlers_test.go`

### Basic Test Structure

```go
func TestSomething(t *testing.T) {
    // Arrange
    expected := "expected value"

    // Act
    result := YourFunction()

    // Assert
    if result != expected {
        t.Errorf("Expected %v, got %v", expected, result)
    }
}
```

**Status:** Testing infrastructure to be set up in [Issue #8](https://github.com/timLP79/cs408-go-stack/issues/8)

## Deployment

(To be documented)

## Contributing

This is a student project for CS408.

## License

(To be determined)

## Acknowledgments

- Based on the [Full Stack Starter](https://github.com/shanep/fullstack-starter) Node.js application
- Built as a learning exercise to understand Go web development
