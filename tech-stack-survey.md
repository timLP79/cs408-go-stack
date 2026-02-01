# CS408 - 03.02 Tech Stack Survey
**Student:** Tim
**Date:** January 30, 2026
**Assignment:** Research & Compare Full-Stack Technologies

---

## Overview

This survey compares three different full-stack web development stacks as alternatives to the default Node.js/Express stack provided in the course. Each stack is evaluated based on backend language, framework, templating system, UI framework, and testing approach.

---

## Stack 1: Go + Gin (My Choice for Final Project)

### Backend Language
**Go 1.22+**
- Statically-typed, compiled language developed by Google
- Built-in concurrency support with goroutines
- Fast compilation and execution
- Strong standard library

### Backend Framework
**Gin Web Framework** (`github.com/gin-gonic/gin`)
- Lightweight, high-performance HTTP web framework
- Clean routing API similar to Express.js
- Built-in middleware support (logging, recovery, CORS)
- JSON validation and rendering
- Grouped routing for API versioning

### Templating System
**Go `html/template`** with layout pattern
- Server-side rendering (SSR)
- Native Go templating engine (no external dependencies)
- Template inheritance via `{{define}}` and `{{block}}` syntax
- Automatic HTML escaping for XSS prevention
- Can be combined with HTMX for dynamic updates

### UX/UI Framework
**Bootstrap 5** (via CDN)
- Responsive grid system
- Pre-built components (navbar, cards, forms, buttons)
- Well-documented and widely supported
- Mobile-first approach
- Easy to customize with Sass variables

### Testing Approach

**Unit Tests:**
- Go's built-in `testing` package
- Table-driven tests for comprehensive coverage
- Benchmarking support built-in

**Integration Tests:**
- `net/http/httptest` package for HTTP handler testing
- Test HTTP requests/responses without running actual server
- Database testing with in-memory SQLite

**E2E Tests (Optional):**
- Selenium or Playwright for browser automation
- Test complete user workflows

**Tools:**
- `go test ./...` - Run all tests
- `go test -v` - Verbose output
- `go test -cover` - Coverage reports
- `go test -bench` - Performance benchmarks

**Example Test:**
```go
func TestGetAllTodos(t *testing.T) {
    db := setupTestDB()
    todos, err := db.GetAllTodos()
    if err != nil {
        t.Errorf("Expected no error, got %v", err)
    }
    if len(todos) != 2 {
        t.Errorf("Expected 2 todos, got %d", len(todos))
    }
}
```

### Debugging Approach

**Print Debugging:**
- `fmt.Println()` - Quick debug output in production code
- `t.Logf()` - Debug output in tests (only shows with `-v` flag)
- `log.Printf()` - Logging with timestamps

**Delve Debugger (Official Go Debugger):**
- `dlv debug` - Start debugging session
- Set breakpoints, inspect variables, step through code
- Command-line interface for debugging
- Install: `go install github.com/go-delve/delve/cmd/dlv@latest`

**VS Code Debugging:**
- Built-in Go debugger integration
- Visual breakpoints and variable inspection
- Debug tests with "Debug Test" code lens
- Launch configurations for debugging servers

**Gin Debug Mode:**
- `gin.SetMode(gin.DebugMode)` - Verbose request logging
- `gin.SetMode(gin.TestMode)` - Quiet mode for tests
- Automatic route listing on startup

**Stack Traces and Panic Recovery:**
- Go provides detailed stack traces on panic
- Gin's recovery middleware catches panics and returns 500
- `runtime/debug.Stack()` for manual stack trace printing

**Example Debug Session:**
```go
// In test - using t.Logf() for debugging
func TestIndexRoute(t *testing.T) {
    // ... setup code ...
    router.ServeHTTP(rr, req)

    // DEBUG: Inspect response
    t.Logf("Status Code: %d", rr.Code)
    t.Logf("Response Body: %s", rr.Body.String())

    // ... assertions ...
}
```

**Tools Used:**
- `delve` (dlv) - Official Go debugger
- VS Code with Go extension
- `fmt` package for print debugging
- `log` package for structured logging
- Gin's built-in debug mode

### Pros
- ✅ Compiled language = fast performance and type safety
- ✅ Single binary deployment (no runtime dependencies)
- ✅ Excellent standard library with built-in HTTP server
- ✅ Built-in testing framework (no Jest/Mocha needed)
- ✅ Great for microservices and concurrent applications
- ✅ Low memory footprint
- ✅ Easy deployment on EC2 Linux instances

### Cons
- ❌ Steeper learning curve for template syntax
- ❌ Smaller ecosystem compared to Node.js/Python
- ❌ More verbose error handling (explicit error returns)
- ❌ No dynamic typing flexibility

### Use Cases
- RESTful APIs and microservices
- Cloud-native applications
- High-performance web services
- Real-time applications (WebSockets, SSE)
- DevOps tools and CLI applications

---

## Stack 2: Ruby on Rails

### Backend Language
**Ruby 3.0+**
- Dynamic, object-oriented language
- Readable syntax ("optimized for programmer happiness")
- Duck typing
- Powerful metaprogramming capabilities

### Backend Framework
**Ruby on Rails 7**
- Full-featured MVC framework
- "Convention over Configuration" philosophy
- Built-in ORM (ActiveRecord) for database operations
- Database migrations for version control
- Asset pipeline for CSS/JS management
- Action Cable for WebSockets
- Active Storage for file uploads

### Templating System
**ERB (Embedded Ruby)** or Slim/Haml
- Server-side rendering
- Embedded Ruby syntax: `<%= variable %>` for output, `<% code %>` for logic
- Template inheritance with layouts and partials
- **Alternative:** Hotwire/Turbo for SPA-like experience without heavy JavaScript
- ViewComponents for reusable UI components

### UX/UI Framework
**Bootstrap, Tailwind CSS, or Bulma**
- Rails 7 includes Tailwind CSS by default (via importmaps)
- Often paired with **Stimulus.js** for lightweight JavaScript interactions
- **Hotwire Turbo** for reactive page updates without full page reloads
- ViewComponent library for component-based UI

### Testing Approach

**Unit Tests:**
- **RSpec** - BDD (Behavior-Driven Development) testing framework
- **Minitest** - Rails default testing framework
- Model tests for business logic and validations

**Integration Tests:**
- **RSpec + Capybara** for feature testing
- Simulates user interactions (click buttons, fill forms)
- Tests full request/response cycle

**E2E Tests:**
- **Capybara + Selenium WebDriver** for browser automation
- **Cuprite** (headless Chrome driver) for faster E2E tests
- Tests JavaScript interactions and AJAX calls

**Database Tests:**
- **Database Cleaner** for test isolation
- **FactoryBot** for test data generation
- Transactional fixtures for fast rollback

**Tools:**
- `rspec` - Run RSpec tests
- `rails test` - Run Minitest tests
- `SimpleCov` - Code coverage reporting
- `Guard` - Automated test running on file changes

**Example Test:**
```ruby
RSpec.describe TodosController, type: :controller do
  describe "GET #index" do
    it "returns a successful response" do
      get :index
      expect(response).to be_successful
    end
  end
end
```

### Debugging Approach

**byebug (Interactive Debugger):**
- Add `byebug` anywhere in your code to set breakpoint
- Interactive REPL for inspecting variables
- Step through code, continue execution, examine call stack
- Built into Rails by default

**Pry (Alternative Debugger):**
- Add `binding.pry` to pause execution
- More powerful REPL than byebug
- Syntax highlighting and better introspection
- Install: `gem install pry-byebug`

**Rails Logs:**
- Automatic request/response logging in `log/development.log`
- See SQL queries, render times, parameters
- Use `Rails.logger.debug` for custom log messages
- Tail logs: `tail -f log/development.log`

**Better Errors Gem:**
- Enhanced error pages in development
- Shows live REPL in browser at error location
- Inspect local variables and application state
- Install: `gem 'better_errors'` and `gem 'binding_of_caller'`

**Web Console:**
- Interactive console in browser on exception pages
- Inspect variables and execute Ruby code
- Built into Rails in development mode

**Example Debug Session:**
```ruby
def index
  @todos = Todo.all
  byebug  # Execution pauses here
  render :index
end

# In byebug console:
# > @todos.count
# > @todos.first.inspect
# > continue  # Resume execution
```

**Tools Used:**
- `byebug` - Standard Ruby debugger
- `pry` - Enhanced REPL
- `better_errors` gem - Better error pages
- Rails logger - Application logging
- `rails console` - Interactive Rails shell

### Pros
- ✅ Extremely productive for CRUD applications
- ✅ Massive gem ecosystem (Ruby libraries)
- ✅ Strong conventions reduce decision fatigue
- ✅ Excellent for rapid prototyping
- ✅ Great testing culture and comprehensive tools
- ✅ Active Record makes database operations simple
- ✅ Built-in authentication, authorization, caching

### Cons
- ❌ Slower runtime performance than compiled languages
- ❌ "Magic" conventions can be hard to debug for beginners
- ❌ Less popular than it used to be (smaller job market than Node/Python)
- ❌ Monolithic architecture (harder to extract microservices)
- ❌ Upgrading Rails versions can be painful

### Use Cases
- MVPs and rapid prototyping
- Content management systems
- E-commerce platforms (Shopify runs on Rails)
- Social networks and community platforms
- Internal business applications

---

## Stack 3: ASP.NET Core (C#)

### Backend Language
**C# 12+** (.NET 8)
- Modern, statically-typed, object-oriented language
- LINQ for querying data
- Async/await for asynchronous programming
- Strong type safety with nullable reference types
- Pattern matching and records

### Backend Framework
**ASP.NET Core MVC**
- Cross-platform, open-source framework (runs on Linux/Mac/Windows)
- Built-in dependency injection container
- Strong MVC pattern implementation
- Entity Framework Core ORM for database access
- SignalR for real-time communication
- Minimal APIs for lightweight HTTP services

### Templating System
**Razor Pages** or **Razor Views**
- Server-side rendering with `@` syntax
- Strongly typed views (IntelliSense support in IDE)
- Partial views and layouts with `@RenderBody()`
- Tag Helpers for cleaner HTML
- **Alternative:** **Blazor** for component-based UI (server-side or WebAssembly)

**Example Razor Syntax:**
```html
@model TodoViewModel
<h1>@Model.Title</h1>
@foreach (var todo in Model.Todos) {
    <p>@todo.Task</p>
}
```

### UX/UI Framework
**Bootstrap** (default), **Tailwind CSS**, or **Blazor Components**
- ASP.NET scaffolding includes Bootstrap 5 by default
- Can integrate Material UI, Bulma, or custom CSS frameworks
- **Blazor** offers component-based development similar to React
- **MudBlazor** or **Radzen** component libraries for Blazor

### Testing Approach

**Unit Tests:**
- **xUnit** - Modern, extensible testing framework (recommended)
- **NUnit** - Popular alternative
- **MSTest** - Microsoft's testing framework
- Tests for controllers, services, models

**Integration Tests:**
- **WebApplicationFactory** for in-memory testing
- Tests entire HTTP pipeline without external server
- `TestServer` for request/response testing

**E2E Tests:**
- **Selenium WebDriver** with **SpecFlow** (BDD framework)
- **Playwright for .NET** - Modern browser automation
- Tests full user workflows across browsers

**Database Tests:**
- **In-memory database provider** (EF Core)
- **SQLite** in-memory mode for fast tests
- **Respawn** library for database cleanup between tests

**Tools:**
- `dotnet test` - CLI test runner
- **Visual Studio Test Explorer** - GUI test runner
- **Moq** - Mocking framework for dependencies
- **FluentAssertions** - Readable assertions
- **Coverlet** - Code coverage tool
- **BenchmarkDotNet** - Performance benchmarking

**Example Test:**
```csharp
[Fact]
public async Task GetAllTodos_ReturnsOkResult()
{
    // Arrange
    var controller = new TodosController(_mockRepo.Object);

    // Act
    var result = await controller.GetAllTodos();

    // Assert
    Assert.IsType<OkObjectResult>(result);
}
```

### Debugging Approach

**Visual Studio Debugger (Most Powerful):**
- Full-featured IDE debugger
- Visual breakpoints - click in margin to set
- Watch windows for monitoring variables
- Immediate window for executing code during debug
- IntelliTrace for historical debugging
- Hot Reload - change code while debugging

**VS Code Debugger:**
- Lightweight alternative to Visual Studio
- F5 to start debugging
- Breakpoints, watch variables, call stack
- Works on Linux/Mac/Windows
- launch.json configuration for custom debug settings

**Logging with ILogger:**
- Built-in dependency injection for logging
- Multiple log levels (Debug, Info, Warning, Error)
- Providers for console, file, Application Insights
- Structured logging support

**Debug vs Release Mode:**
- Debug mode: includes symbols, no optimization
- Release mode: optimized, smaller binaries
- Configure in launchSettings.json

**Developer Exception Page:**
- Detailed error pages in development
- Shows stack trace, source code, variables
- Enabled automatically in Development environment

**Example Debug Session:**
```csharp
public class TodosController : ControllerBase
{
    private readonly ILogger<TodosController> _logger;

    public async Task<IActionResult> GetAllTodos()
    {
        _logger.LogDebug("Fetching all todos");
        var todos = await _repository.GetAllTodos();

        // Set breakpoint here - F9 in Visual Studio
        _logger.LogInformation("Found {Count} todos", todos.Count);

        return Ok(todos);
    }
}
```

**Tools Used:**
- **Visual Studio** - Full IDE with advanced debugging
- **VS Code** - Lightweight debugger
- **dotnet watch** - Auto-restart on file changes
- **ILogger** - Built-in logging framework
- **Application Insights** - Production monitoring
- **MiniProfiler** - Performance profiling

### Pros
- ✅ Strong typing with excellent IDE support (IntelliSense, refactoring)
- ✅ High performance (compiled, optimized runtime)
- ✅ Enterprise-grade features (security, caching, logging, monitoring)
- ✅ Great for complex business applications
- ✅ Cross-platform (runs on Linux for EC2 deployment)
- ✅ Excellent async/await support
- ✅ Strong Microsoft ecosystem and documentation

### Cons
- ❌ Steeper learning curve (C# syntax, .NET ecosystem)
- ❌ Heavier than lightweight frameworks like Express/Gin
- ❌ More ceremony/boilerplate than Ruby/Python frameworks
- ❌ Smaller community outside enterprise environments
- ❌ Visual Studio (full IDE) is Windows-only (though VS Code works)

### Use Cases
- Enterprise applications and line-of-business systems
- Financial services and banking applications
- Healthcare applications (HIPAA compliance)
- Large-scale e-commerce platforms
- Real-time applications (SignalR for chat, notifications)
- Microservices architectures

---

## Comparison Summary

| Aspect | Go + Gin | Ruby on Rails | ASP.NET Core (C#) |
|--------|----------|---------------|-------------------|
| **Language** | Go | Ruby | C# |
| **Type System** | Static, compiled | Dynamic, interpreted | Static, compiled |
| **Performance** | ⭐⭐⭐⭐⭐ Very Fast | ⭐⭐⭐ Medium | ⭐⭐⭐⭐⭐ Very Fast |
| **Learning Curve** | ⭐⭐⭐ Moderate | ⭐⭐ Easy | ⭐⭐⭐⭐ Steep |
| **Ecosystem Size** | ⭐⭐⭐ Good | ⭐⭐⭐⭐ Large (gems) | ⭐⭐⭐⭐ Large (NuGet) |
| **Productivity** | ⭐⭐⭐ Good | ⭐⭐⭐⭐⭐ Very High | ⭐⭐⭐⭐ High |
| **Deployment** | ⭐⭐⭐⭐⭐ Single binary | ⭐⭐⭐ Ruby + gems | ⭐⭐⭐⭐ Self-contained |
| **Testing Tools** | ⭐⭐⭐⭐⭐ Built-in | ⭐⭐⭐⭐⭐ Excellent | ⭐⭐⭐⭐⭐ Excellent |
| **Enterprise Use** | ⭐⭐⭐⭐ Growing | ⭐⭐⭐ Declining | ⭐⭐⭐⭐⭐ Very Popular |
| **Cloud Native** | ⭐⭐⭐⭐⭐ Excellent | ⭐⭐⭐ Good | ⭐⭐⭐⭐ Excellent |
| **Community Size** | ⭐⭐⭐⭐ Large | ⭐⭐⭐⭐ Large | ⭐⭐⭐⭐ Large |
| **Job Market** | ⭐⭐⭐⭐ Growing | ⭐⭐⭐ Stable | ⭐⭐⭐⭐⭐ Very Strong |

---

## My Choice: Go + Gin

For my CS408 final project, I have chosen **Go + Gin** for the following reasons:

### 1. Learning Goals
- I wanted to learn a modern, compiled, statically-typed language beyond JavaScript
- Go's simplicity makes it approachable while teaching important concepts like concurrency
- Understanding compiled languages prepares me for systems programming

### 2. Performance
- Go compiles to native machine code, making it significantly faster than interpreted languages
- Excellent for building responsive web applications and APIs
- Low latency is important for user experience

### 3. Simplicity & Clarity
- Go's syntax is clean and straightforward (no classes, just structs and functions)
- Explicit error handling (no hidden exceptions)
- Minimal "magic" - easier to understand what's happening under the hood

### 4. Built-in Testing
- Testing is a first-class citizen in Go
- No need to choose and configure external testing frameworks
- Simple syntax: `go test ./...` runs all tests
- Built-in benchmarking and profiling

### 5. Deployment & DevOps
- **Single binary deployment:** Compile once, deploy anywhere
- No runtime dependencies (no Node.js, Ruby, or .NET runtime needed)
- Small Docker images (can use `FROM scratch`)
- Perfect for EC2 Linux deployment

### 6. Career & Industry Relevance
- Go is increasingly popular for:
  - Cloud-native applications (Kubernetes, Docker, Terraform written in Go)
  - Microservices architectures
  - DevOps tools and CLI applications
  - Backend APIs at companies like Google, Uber, Dropbox
- Strong job market growth in backend/cloud engineering roles

### 7. Scalability
- Built-in concurrency with goroutines and channels
- Handles many simultaneous connections efficiently
- Great for building scalable web services

---

## EC2 Linux Compatibility

All three stacks are compatible with EC2 Linux deployment:

| Stack | EC2 Deployment |
|-------|----------------|
| **Go + Gin** | ✅ Single binary, no dependencies. Upload and run. |
| **Ruby on Rails** | ✅ Install Ruby runtime, gems, configure web server (Puma/Unicorn). |
| **ASP.NET Core** | ✅ Install .NET runtime or use self-contained deployment. |

---

## Conclusion

This survey explored three distinct full-stack development approaches:
- **Go + Gin:** Modern, performant, cloud-native
- **Ruby on Rails:** Productive, convention-driven, rapid development
- **ASP.NET Core:** Enterprise-grade, strongly-typed, Microsoft ecosystem

Each stack has its strengths, and the choice depends on project requirements, team expertise, and deployment constraints. For my learning goals and the EC2 deployment requirement, **Go + Gin** offers the best balance of performance, simplicity, and modern cloud-native capabilities.

---

**End of Survey**
