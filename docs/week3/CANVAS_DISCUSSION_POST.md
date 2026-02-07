# CS408 - Tech Stack Survey

---

## Stack 1: Go + Gin

**Backend language:** Go 1.22+
**Framework:** Gin Web Framework
**Templating/rendering:** Go `html/template` (server-side rendering)
**UX/UI:** Bootstrap 5 (CDN)
**Testing:** Built-in `testing` package, `httptest` for HTTP testing
**Debugging:** Delve debugger, JetBrains GoLand integration, `t.Logf()` for test debugging

**Pros/cons:**
- ✅ Fast compiled binary, single deployment file
- ✅ Built-in testing, no external tools needed
- ❌ Smaller ecosystem than Node/Python

---

## Stack 2: Ruby on Rails

**Backend language:** Ruby 3.0+
**Framework:** Ruby on Rails 7
**Templating/rendering:** ERB (Embedded Ruby)
**UX/UI:** Bootstrap/Tailwind with Stimulus.js
**Testing:** RSpec or Minitest, Capybara for integration tests
**Debugging:** byebug interactive debugger, Rails logs, Better Errors gem

**Pros/cons:**
- ✅ Extremely productive, convention over configuration
- ✅ Massive gem ecosystem
- ❌ Slower performance than compiled languages

---

## Stack 3: ASP.NET Core (C#)

**Backend language:** C# 12+ (.NET 8)
**Framework:** ASP.NET Core MVC
**Templating/rendering:** Razor Pages/Views
**UX/UI:** Bootstrap (default)
**Testing:** xUnit, NUnit, or MSTest with WebApplicationFactory
**Debugging:** Visual Studio debugger, VS Code, ILogger framework

**Pros/cons:**
- ✅ Strong typing, excellent IDE support
- ✅ Enterprise-grade, high performance
- ❌ Steeper learning curve

---

## Chosen Stack to Do Hello World

**I chose:** Go + Gin

**Reason:**
I chose Go + Gin to learn a modern compiled language with strong typing. Go's single binary deployment makes EC2 deployment simple with no runtime dependencies. The built-in testing framework eliminates external tool configuration. Go is increasingly popular for cloud-native applications and microservices, making it valuable for my career. The simplicity and fast performance make it ideal for learning full-stack development.

---

## Hello World Evidence

### Route/controller:

**main.go:**
```go
func handleIndex(c *gin.Context) {
    c.Writer.WriteHeader(200)
    templates["index"].ExecuteTemplate(c.Writer, "layout", gin.H{
        "Title": "Hello World",
    })
}
```

### Template/view:

**templates/index.html:**
```html
{{define "content"}}
<div class="text-center">
    <h1 class="display-1 text-primary">{{.Title}}</h1>
    <p class="lead">Welcome to my Go full-stack web application!</p>
    <button class="btn btn-success btn-lg">Click Me!</button>
</div>
{{end}}
```

### UI framework proof:

Bootstrap 5 via CDN with classes: `display-1`, `text-primary`, `text-center`, `lead`, `btn btn-success btn-lg`

### Test + results:

**main_test.go:**
```go
func TestIndexRoute(t *testing.T) {
    // ... setup ...
    router.ServeHTTP(rr, req)

    if rr.Code != http.StatusOK {
        t.Errorf("Expected 200, got %v", rr.Code)
    }
    if !strings.Contains(rr.Body.String(), "Hello World") {
        t.Error("Response missing 'Hello World'")
    }
}
```

**Results:** Test passes - validates 200 status and "Hello World" in response

### Debugging:

**Issue:** Test failed with empty response body
**Process:** Added `t.Logf()` debug output → Found template execution mismatch → Fixed test to match production code → Test passed
**Tools:** `go test -v`, `t.Logf()`, command-line inspection

### Screenshots/Repo link:

**GitHub:** https://github.com/timLP79/cs408-go-stack

**Screenshots in repo (`/screenshots/`):**
1. Server running locally
2. Browser with Bootstrap UI
3. Test passing
4. Debugging: test failure
5. Debugging: debug output

---

**Summary:** Fully functional Go + Gin Hello World app with Bootstrap UI, automated tests, and documented debugging process. All code and documentation available in GitHub repository.
