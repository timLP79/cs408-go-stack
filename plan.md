# Plan: Rebuild Full-Stack Starter in Go

## Tech Stack

| Layer | Original (Node.js) | Go Replacement |
|-------|-------------------|----------------|
| Framework | Express.js | **Gin** (`github.com/gin-gonic/gin`) |
| Templating | EJS + express-ejs-layouts | **Go `html/template`** with layout pattern |
| Database | better-sqlite3 | **modernc.org/sqlite** (pure Go, no CGo) |
| CSS | Bootstrap 5 CDN | Same (no change) |
| Static files | `express.static()` | `gin.Static()` / `gin.StaticFile()` |

## Target Directory Structure

```
go-full-stack/
├── main.go              # Entry point: config, router, template loading, server
├── db.go                # DatabaseManager struct + all 10 helper methods
├── handlers.go          # HTTP handler functions (HandleIndex, HandleNotFound)
├── templates/
│   ├── layout.html      # Base layout (replaces layout.ejs)
│   ├── index.html       # Landing page content block
│   └── error.html       # Error page content block
├── static/
│   ├── lab0.html        # Copied from original
│   ├── favicon.svg      # Copied from original
│   ├── images/
│   │   └── dependency_2x.png
│   ├── javascripts/
│   │   └── myscript.js
│   └── stylesheets/
│       ├── style.css
│       └── lab0.css
├── go.mod
├── go.sum
└── .gitignore
```

## Implementation Steps

### Step 1: Project skeleton

- Run `go mod init go-full-stack` in the `go-full-stack/` directory
- Run `go get github.com/gin-gonic/gin` and `go get modernc.org/sqlite`
- Create directory structure: `templates/`, `static/`, `static/images/`, `static/javascripts/`, `static/stylesheets/`

### Step 2: Copy static assets verbatim

Copy these files from the original app unchanged:

| Source | Destination |
|--------|-------------|
| `app/src/static/lab0.html` | `static/lab0.html` |
| `app/src/public/images/favicon.svg` | `static/favicon.svg` |
| `app/src/public/images/dependency_2x.png` | `static/images/dependency_2x.png` |
| `app/src/public/javascripts/myscript.js` | `static/javascripts/myscript.js` |
| `app/src/public/stylesheets/style.css` | `static/stylesheets/style.css` |

Also check for `lab0.css` and copy if present.

### Step 3: Create templates

**`templates/layout.html`** - Go equivalent of `layout.ejs`:
- Define a `{{define "layout"}}` block wrapping the full HTML document
- Use `{{block "content" .}}{{end}}` where `<%- body %>` was
- Keep identical Bootstrap 5 CDN links, footer, year script, and myscript.js include
- Use `{{.Title}}` instead of `<%= title %>`

**`templates/index.html`** - Go equivalent of `index.ejs`:
- Define only `{{define "content"}}...{{end}}`
- Keep identical hero section HTML, just replace `<%= title %>` with `{{.Title}}`

**`templates/error.html`**:
- Define `{{define "content"}}...{{end}}` with error message and status

### Step 4: Create `main.go`

Key responsibilities:
- Read `PORT` env var (default `"3000"`), `DATA_DIR` (default `"data"`), `DB_NAME` (default `"database.sqlite"`)
- Initialize `DatabaseManager`
- Load templates using a `map[string]*template.Template` — parse each page paired with `layout.html`
- Provide `renderTemplate()` helper that calls `tmpl.ExecuteTemplate(c.Writer, "layout", data)`
- Set up Gin router:
  - `router.Static("/stylesheets", "static/stylesheets")`
  - `router.Static("/javascripts", "static/javascripts")`
  - `router.Static("/images", "static/images")`
  - `router.StaticFile("/favicon.svg", "static/favicon.svg")`
  - `router.StaticFile("/lab0.html", "static/lab0.html")`
  - `router.GET("/", HandleIndex)`
  - `router.NoRoute(HandleNotFound)`
- Add `DatabaseMiddleware` that stores `*DatabaseManager` in Gin context
- Start server on configured port

### Step 5: Create `handlers.go`

- `HandleIndex`: renders "index" template with `Title: "Full Stack Starter Code"`
- `HandleNotFound`: renders "error" template with 404 status
- `DatabaseMiddleware`: Gin middleware that calls `c.Set("db", dm)`
- `getDB` helper: retrieves `*DatabaseManager` from context

### Step 6: Create `db.go`

- `Todo` struct with `ID int64`, `Task string`, `Completed bool`
- `DatabaseManager` struct wrapping `*sql.DB`
- `NewDatabaseManager(dbPath)`: opens SQLite, enables foreign keys, creates `todos` table
- 10 methods matching the original JS helpers:
  - `GetAllTodos()`, `GetTodoByID()`, `CreateTodo()`, `UpdateTodo()`, `DeleteTodo()`
  - `ToggleTodo()`, `GetTotalTodos()`, `GetCompletedTodos()`
  - `ClearDatabase()`, `SeedTestData()` (both gated on `GO_ENV=test`)
- Handle SQLite `INTEGER` <-> Go `bool` conversion when scanning `completed`

### Step 7: Create `.gitignore`

Ignore `data/`, compiled binary, IDE files, OS files.

### Step 8: Final tidying

- Run `go mod tidy`
- Run `go vet ./...`

## Key Design Decisions

1. **Template layout pattern**: Since Gin's `LoadHTMLGlob` doesn't support template inheritance, we manually parse each page template paired with `layout.html` and store them in a `map[string]*template.Template`. The `renderTemplate` helper executes the `"layout"` template, which pulls in the page's `"content"` block. No extra dependency needed.

2. **Static file routing**: Can't mount `router.Static("/", "static/")` because it conflicts with `router.GET("/", ...)`. Instead, mount each subdirectory and individual root-level file separately.

3. **Flat package structure**: All `.go` files in `package main`. The app is small enough that splitting into `internal/` packages would be premature.

4. **Database driver name**: `modernc.org/sqlite` registers as driver `"sqlite"` (not `"sqlite3"`). The import is `_ "modernc.org/sqlite"` and open call is `sql.Open("sqlite", dbPath)`.

## Verification

1. Run `go build -o go-full-stack .` — should compile without errors
2. Run `./go-full-stack` — server starts on port 3000
3. Visit `http://localhost:3000` — landing page renders with Bootstrap styling, gradient background, hero section
4. Click "Go to Lab 0 Page" — `lab0.html` loads correctly with its own styling
5. Click "Hello World" button on lab0 page — JavaScript alert fires
6. Check browser dev tools — favicon, CSS, JS, and image all load (no 404s)
7. Visit a non-existent route like `/foobar` — error page renders with 404
8. Verify `data/database.sqlite` was created with correct schema: `sqlite3 data/database.sqlite ".schema"`
