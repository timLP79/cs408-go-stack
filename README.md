# Go Full-Stack Web Application

A simple full-stack web application built with Go, Gin web framework, and SQLite database.

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

1. Make changes to `.go` files or templates
2. Run `go build -o go-full-stack .`
3. Run `./go-full-stack`
4. Test at `http://localhost:3000`

Or use `go run .` to skip the build step during development.

## Testing

(To be implemented)

```bash
go test ./...
```

## Deployment

(To be documented)

## Contributing

This is a student project for CS408.

## License

(To be determined)

## Acknowledgments

- Based on the [Full Stack Starter](https://github.com/shanep/fullstack-starter) Node.js application
- Built as a learning exercise to understand Go web development
