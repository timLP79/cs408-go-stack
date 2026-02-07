# Go Learning Guide - Building a Web App

This guide teaches Go syntax and concepts as we build a web application together.

---

## Understanding `main.go` - Line by Line

Let's build this file together and I'll explain each concept:

### Part 1: The `package` declaration

```go
package main
```

**What this means:**
- Every Go file starts with a `package` declaration
- `package main` is special — it means "this is an executable program"
- Other packages (like `package database` or `package handlers`) are libraries
- Only `package main` can have a `func main()` that runs when you execute the program

### Part 2: Imports

```go
import (
	"github.com/gin-gonic/gin"
	"html/template"
)
```

**What this means:**
- `import` brings in code from other packages
- Parentheses `( )` let you import multiple packages at once
- `"html/template"` — built into Go (part of standard library)
- `"github.com/gin-gonic/gin"` — external package you installed with `go get`
- The last part of the path (`gin`, `template`) is how you use them in code

**How you use imports:**
```go
router := gin.Default()        // "gin" comes from the import path
tmpl := template.New("foo")    // "template" comes from the import path
```

### Part 3: Package-level variable

```go
var templates map[string]*template.Template
```

**Breaking this down:**
- `var` declares a variable
- `templates` is the variable name
- `map[string]*template.Template` is the type

**What's a map?**
- Like a dictionary/object in JavaScript: key → value
- `map[string]...` means "keys are strings"
- `*template.Template` means "values are pointers to Template objects"

**What's a pointer (`*`)?**
- In Go, `*` means "a pointer to" (memory address)
- `template.Template` is a value (makes a copy)
- `*template.Template` is a reference (points to the original)
- For large objects like templates, pointers are more efficient

**Why is this outside any function?**
- Variables declared at package level are accessible to all functions in the file
- This will be shared between `main()` and `handleIndex()`

### Part 4: The `main()` function

```go
func main() {
```

**What this means:**
- `func` declares a function
- `main()` is special — it runs automatically when you execute the program
- No parameters `()` and no return type

### Part 5: Initialize the map

```go
	templates = make(map[string]*template.Template)
```

**Breaking this down:**
- `make()` is a built-in function that creates maps, slices, and channels
- `map[string]*template.Template` is the type (same as the variable declaration)
- Without `make()`, the map would be `nil` (null) and you'd crash when trying to use it
- Now `templates` is an empty map ready to store key-value pairs

### Part 6: Parse templates

```go
	templates["index"] = template.Must(template.ParseFiles(
		"templates/layout.html",
		"templates/index.html",
	))
```

**Breaking this down piece by piece:**

**`template.ParseFiles("templates/layout.html", "templates/index.html")`**
- Reads both files and combines them into one template
- Returns two values: `(*template.Template, error)`
- Go functions can return multiple values!

**`template.Must(...)`**
- Wraps the result from `ParseFiles`
- If there's an error, it panics (crashes the program immediately)
- If no error, it returns just the `*template.Template` (strips off the error)
- Use `Must()` when you want the program to crash at startup if templates are broken

**`templates["index"] = ...`**
- Stores the parsed template in the map with key `"index"`
- Later you can retrieve it: `templates["index"]`

### Part 7: Create the router

```go
	router := gin.Default()
```

**Breaking this down:**
- `:=` is short variable declaration (combines `var router ...` and assignment)
- `gin.Default()` creates a new web server with logging and error recovery built in
- `router` is now a `*gin.Engine` (the router type)

**Note:** You can only use `:=` inside functions, not at package level.

### Part 8: Register a route

```go
	router.GET("/", handleIndex)
```

**What this means:**
- Register a handler for `GET` requests to the path `"/"`
- `handleIndex` is the function that will run (we define it below)
- Note: We pass the function itself, not `handleIndex()` (don't call it here)

### Part 9: Start the server

```go
	router.Run(":3000")
```

**What this means:**
- Start the HTTP server on port 3000
- `":3000"` means "listen on all network interfaces, port 3000"
- This function blocks forever (keeps running until you Ctrl+C)

**End of `main()`:**
```go
}
```

---

## The Handler Function

```go
func handleIndex(c *gin.Context) {
```

**Breaking this down:**
- `func handleIndex` declares a function named `handleIndex`
- `(c *gin.Context)` — takes one parameter:
  - Name: `c`
  - Type: `*gin.Context` (pointer to a Context)
  - `gin.Context` contains the HTTP request, response, and helper methods
- No return type — handlers don't return anything, they write to the response

### Setting the HTTP status

```go
	c.Writer.WriteHeader(200)
```

**What this means:**
- `c.Writer` is the HTTP response writer
- `WriteHeader(200)` sets the status code to 200 (OK)
- You could use `404`, `500`, etc.

### Rendering the template

```go
	templates["index"].ExecuteTemplate(c.Writer, "layout", gin.H{
		"Title": "Hello World",
	})
```

**Breaking this down:**

**`templates["index"]`**
- Look up the template we stored earlier
- Returns a `*template.Template`

**`.ExecuteTemplate(c.Writer, "layout", ...)`**
- `ExecuteTemplate` renders a specific template by name
- `c.Writer` — where to write the HTML output (the HTTP response)
- `"layout"` — which template definition to execute (from `{{define "layout"}}`)
- Third parameter is the data to pass to the template

**`gin.H{ ... }`**
- `gin.H` is a shortcut type for `map[string]interface{}`
- `interface{}` means "any type" (like `any` in TypeScript)
- This creates a map with one key-value pair: `"Title"` → `"Hello World"`
- In the template, `{{.Title}}` will become `"Hello World"`

**End of function:**
```go
}
```

---

## Go Syntax Summary

| Concept | Syntax | Example |
|---------|--------|---------|
| Package declaration | `package name` | `package main` |
| Import | `import "path"` or `import ( ... )` | `import "html/template"` |
| Variable declaration | `var name type` | `var templates map[string]*template.Template` |
| Short variable declaration | `name := value` | `router := gin.Default()` |
| Function declaration | `func name(params) returnType { }` | `func handleIndex(c *gin.Context) { }` |
| Map type | `map[keyType]valueType` | `map[string]*template.Template` |
| Pointer type | `*Type` | `*gin.Context` |
| Create map | `make(map[K]V)` | `make(map[string]*template.Template)` |
| Map assignment | `mapName[key] = value` | `templates["index"] = ...` |
| Call function | `functionName(args)` | `gin.Default()` |
| Method call | `object.Method(args)` | `router.GET("/", handleIndex)` |
| Composite literal (struct/map) | `Type{field: value}` | `gin.H{"Title": "Hello World"}` |

---

## Key Go Concepts

1. **Strong typing** — every variable has a specific type
2. **Multiple return values** — functions can return `(value, error)`
3. **Pointers** — `*Type` is a reference, not a copy
4. **Short declarations** — `:=` infers the type automatically
5. **First-class functions** — you can pass functions as arguments
6. **Package-level vs function-level** — `var` at top is shared, `:=` is local

---

## Complete `main.go` Code

```go
package main

import (
	"github.com/gin-gonic/gin"
	"html/template"
)

var templates map[string]*template.Template

func main() {
	// Load templates
	templates = make(map[string]*template.Template)
	templates["index"] = template.Must(template.ParseFiles(
		"templates/layout.html",
		"templates/index.html",
	))

	// Setup router
	router := gin.Default()
	router.GET("/", handleIndex)

	// Start server
	router.Run(":3000")
}

func handleIndex(c *gin.Context) {
	c.Writer.WriteHeader(200)
	templates["index"].ExecuteTemplate(c.Writer, "layout", gin.H{
		"Title": "Hello World",
	})
}
```

---

## Next Steps

- Template syntax (HTML templates with Go)
- Adding more routes
- Working with the database
- Error handling in Go
