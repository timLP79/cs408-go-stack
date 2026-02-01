# Go Testing and Debugging Guide

**CS408 Spring 2026** | Reference Tutorial for Go Full-Stack Project

---

## Table of Contents
1. [Introduction to Testing in Go](#introduction-to-testing-in-go)
2. [Writing Your First Test](#writing-your-first-test)
3. [Running Tests](#running-tests)
4. [Understanding Test Output](#understanding-test-output)
5. [Debugging Techniques](#debugging-techniques)
6. [Common Testing Patterns](#common-testing-patterns)
7. [Best Practices](#best-practices)

---

## Introduction to Testing in Go

### Why Test?

Testing ensures your code:
- ✅ Works as expected
- ✅ Doesn't break when you make changes
- ✅ Handles edge cases and errors
- ✅ Is documented (tests show how to use your code)

### Go's Testing Philosophy

Go includes testing in the standard library - no external tools needed!

**Key principles:**
- Simple, readable tests
- Table-driven tests for multiple cases
- Built-in benchmarking
- Fast test execution

### Test File Naming Convention

| Source File | Test File |
|-------------|-----------|
| `main.go` | `main_test.go` |
| `db.go` | `db_test.go` |
| `handlers.go` | `handlers_test.go` |

**Rules:**
- Test files end with `_test.go`
- Test files live in the same directory as the code they test
- Test files use the same package name (e.g., `package main`)

---

## Writing Your First Test

### Test Function Structure

Every test function follows this pattern:

```go
func TestSomething(t *testing.T) {
    // 1. Arrange - Set up test data
    // 2. Act - Run the code being tested
    // 3. Assert - Check the results
}
```

**Naming rules:**
- Function name starts with `Test`
- Takes one parameter: `t *testing.T`
- Uses `t.Errorf()` to report failures
- Uses `t.Log()` to report success (only visible with `-v` flag)

### Example: Testing the Index Route

**File: `main_test.go`**

```go
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestIndexRoute tests the main index route
func TestIndexRoute(t *testing.T) {
	// ARRANGE: Set up the test environment
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	router.LoadHTMLGlob("templates/*")

	router.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", gin.H{
			"Title": "Full Stack Starter Code",
		})
	})

	// ACT: Create and execute a fake HTTP request
	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// ASSERT: Check the results
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	expectedText := "Full Stack Starter Code"
	if !strings.Contains(rr.Body.String(), expectedText) {
		t.Errorf("Handler returned unexpected body: expected to contain %q",
			expectedText)
	}

	t.Log("✅ Index route test passed!")
}
```

### Understanding Each Part

#### 1. Import Required Packages

```go
import (
	"net/http"          // HTTP constants (StatusOK, StatusNotFound, etc.)
	"net/http/httptest" // Test HTTP requests without a real server
	"strings"           // String operations (Contains, etc.)
	"testing"           // Go's testing framework

	"github.com/gin-gonic/gin" // Your web framework
)
```

#### 2. Set Up Test Environment

```go
gin.SetMode(gin.TestMode)
```
- Suppresses Gin's debug output during tests
- Keeps test output clean and focused

```go
router := gin.Default()
router.LoadHTMLGlob("templates/*")
```
- Creates a new router (separate from your main app)
- Loads templates for testing

#### 3. Create a Fake HTTP Request

```go
req, err := http.NewRequest("GET", "/", nil)
```
- `"GET"` - HTTP method
- `"/"` - URL path
- `nil` - Request body (none for GET requests)

**Important:** This is a FAKE request - no real server needed!

#### 4. Record the Response

```go
rr := httptest.NewRecorder()
router.ServeHTTP(rr, req)
```
- `httptest.NewRecorder()` creates a fake response writer
- `ServeHTTP()` runs your handler and captures the response
- Response is stored in `rr` (response recorder)

#### 5. Assert Results

```go
if status := rr.Code; status != http.StatusOK {
    t.Errorf("Handler returned wrong status code: got %v want %v",
        status, http.StatusOK)
}
```
- Checks the HTTP status code
- Uses `t.Errorf()` to report failures (test continues)

```go
if !strings.Contains(rr.Body.String(), expectedText) {
    t.Errorf("Handler returned unexpected body: expected to contain %q",
        expectedText)
}
```
- Checks the response body contains expected text
- `rr.Body.String()` converts response body to string

---

## Running Tests

### Basic Test Commands

| Command | Description |
|---------|-------------|
| `go test` | Run tests in current directory |
| `go test ./...` | Run all tests in project (including subdirectories) |
| `go test -v` | Verbose output (shows all tests and logs) |
| `go test -run TestIndexRoute` | Run only tests matching pattern |
| `go test -cover` | Show code coverage percentage |
| `go test -coverprofile=coverage.out` | Generate coverage report |
| `go test -bench .` | Run benchmark tests |

### Step-by-Step: Running Your First Test

**1. Run the test:**
```bash
go test
```

**Expected output (if passing):**
```
PASS
ok      go-full-stack   0.123s
```

**2. Run with verbose output:**
```bash
go test -v
```

**Expected output:**
```
=== RUN   TestIndexRoute
    main_test.go:56: ✅ Index route test passed!
--- PASS: TestIndexRoute (0.01s)
PASS
ok      go-full-stack   0.123s
```

**3. Run with coverage:**
```bash
go test -cover
```

**Expected output:**
```
PASS
coverage: 42.9% of statements
ok      go-full-stack   0.145s
```

---

## Understanding Test Output

### When Tests Pass

```
=== RUN   TestIndexRoute
--- PASS: TestIndexRoute (0.01s)
PASS
ok      go-full-stack   0.123s
```

**Reading this:**
- `=== RUN` - Test is starting
- `--- PASS` - Test passed
- `(0.01s)` - Time it took
- `ok go-full-stack 0.123s` - All tests in package passed

### When Tests Fail

```
=== RUN   TestIndexRoute
    main_test.go:45: Handler returned wrong status code: got 404 want 200
--- FAIL: TestIndexRoute (0.01s)
FAIL
exit status 1
FAIL    go-full-stack   0.125s
```

**Reading this:**
- `main_test.go:45` - Line number where test failed
- `got 404 want 200` - What went wrong
- `--- FAIL` - Test failed
- `exit status 1` - Non-zero exit (indicates failure)

### Common Error Messages

| Error Message | Meaning | Solution |
|---------------|---------|----------|
| `got 404 want 200` | Route not found | Check router setup |
| `expected to contain "text"` | Response missing text | Check template rendering |
| `Failed to create request` | Request setup failed | Check request parameters |
| `template: pattern matches no files` | Templates not found | Check template path |

---

## Debugging Techniques

### Technique 1: Print Debugging (Simplest)

Add `fmt.Println()` or `t.Logf()` to see values:

```go
func TestIndexRoute(t *testing.T) {
    // ... setup code ...

    router.ServeHTTP(rr, req)

    // DEBUG: Print the status code
    t.Logf("Status Code: %d", rr.Code)

    // DEBUG: Print the response body
    t.Logf("Response Body: %s", rr.Body.String())

    // ... assertions ...
}
```

**Run with `-v` to see logs:**
```bash
go test -v
```

**Output:**
```
=== RUN   TestIndexRoute
    main_test.go:40: Status Code: 200
    main_test.go:43: Response Body: <html>...</html>
--- PASS: TestIndexRoute (0.01s)
```

### Technique 2: Intentionally Fail to See Values

Temporarily fail a test to inspect values:

```go
// Always fails - shows you the actual value
t.Errorf("DEBUG - Status Code: %d", rr.Code)
t.Errorf("DEBUG - Response Body: %s", rr.Body.String())
```

**Run the test:**
```bash
go test -v
```

**You'll see:**
```
--- FAIL: TestIndexRoute (0.01s)
    main_test.go:45: DEBUG - Status Code: 200
    main_test.go:46: DEBUG - Response Body: <html>...</html>
```

Remove these lines when done debugging!

### Technique 3: Using Delve Debugger (Advanced)

**Delve** is Go's official debugger.

**Install Delve:**
```bash
go install github.com/go-delve/delve/cmd/dlv@latest
```

**Debug a test:**
```bash
dlv test
```

**Commands in Delve:**
```
(dlv) break TestIndexRoute    # Set breakpoint at test
(dlv) continue                # Run until breakpoint
(dlv) next                    # Next line
(dlv) step                    # Step into function
(dlv) print rr.Code           # Print variable value
(dlv) print rr.Body.String()  # Print response body
(dlv) quit                    # Exit debugger
```

### Technique 4: JetBrains GoLand Debugging (Easiest for IDE users)

If you're using JetBrains GoLand:

**1. Open `main_test.go`**

**2. Click left of line number to set breakpoint (red dot appears)**

**3. Right-click on the test function and select "Debug"**

**4. Use debug controls:**
- Step Over (F8) - Next line
- Step Into (F7) - Go into function
- Resume Program (F9) - Run to next breakpoint
- Hover over variables to see values
- Use Watches window to monitor expressions
- Evaluate Expression (Alt+F8) to run code during debug

### Technique 5: Table-Driven Test Debugging

For multiple test cases, use a loop:

```go
func TestIndexRoute(t *testing.T) {
    tests := []struct {
        name           string
        path           string
        expectedStatus int
        expectedText   string
    }{
        {"Home page", "/", 200, "Full Stack Starter Code"},
        {"About page", "/about", 200, "About Us"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // ... test code using tt.path, tt.expectedStatus, etc. ...

            if status := rr.Code; status != tt.expectedStatus {
                t.Errorf("%s: got status %v want %v",
                    tt.name, status, tt.expectedStatus)
            }
        })
    }
}
```

**Run specific test:**
```bash
go test -v -run TestIndexRoute/Home
```

---

## Common Testing Patterns

### Pattern 1: Testing HTTP Handlers

```go
func TestHandler(t *testing.T) {
    // Setup
    router := gin.Default()
    router.GET("/path", yourHandler)

    // Create request
    req, _ := http.NewRequest("GET", "/path", nil)
    rr := httptest.NewRecorder()

    // Execute
    router.ServeHTTP(rr, req)

    // Assert
    if rr.Code != http.StatusOK {
        t.Errorf("Expected 200, got %d", rr.Code)
    }
}
```

### Pattern 2: Testing with POST Data

```go
func TestPostHandler(t *testing.T) {
    router := gin.Default()
    router.POST("/submit", submitHandler)

    // Create POST body
    body := strings.NewReader(`{"name":"test"}`)
    req, _ := http.NewRequest("POST", "/submit", body)
    req.Header.Set("Content-Type", "application/json")

    rr := httptest.NewRecorder()
    router.ServeHTTP(rr, req)

    if rr.Code != http.StatusCreated {
        t.Errorf("Expected 201, got %d", rr.Code)
    }
}
```

### Pattern 3: Table-Driven Tests

```go
func TestCalculate(t *testing.T) {
    tests := []struct {
        name     string
        input    int
        expected int
    }{
        {"Zero", 0, 0},
        {"Positive", 5, 10},
        {"Negative", -5, -10},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := Calculate(tt.input)
            if result != tt.expected {
                t.Errorf("got %d want %d", result, tt.expected)
            }
        })
    }
}
```

### Pattern 4: Testing Error Conditions

```go
func TestErrorHandling(t *testing.T) {
    result, err := FunctionThatMightFail()

    // Test that error occurred
    if err == nil {
        t.Error("Expected error, got nil")
    }

    // Test error message
    expectedMsg := "invalid input"
    if err.Error() != expectedMsg {
        t.Errorf("Expected error %q, got %q", expectedMsg, err.Error())
    }
}
```

### Pattern 5: Setup and Teardown

```go
func TestWithSetup(t *testing.T) {
    // Setup
    db := setupTestDatabase()
    defer db.Close() // Teardown after test

    // Test code
    result := db.Query("SELECT * FROM todos")

    if result == nil {
        t.Error("Query failed")
    }
}
```

---

## Best Practices

### ✅ DO

1. **Write tests as you code** (Test-Driven Development)
   - Write test first (it fails)
   - Write minimal code to pass
   - Refactor

2. **Use descriptive test names**
   ```go
   // Good
   func TestIndexRoute_ReturnsHTMLWithTitle(t *testing.T)

   // Bad
   func TestIndex(t *testing.T)
   ```

3. **Test one thing per test**
   ```go
   // Good - separate tests
   func TestStatusCode(t *testing.T) { /* ... */ }
   func TestResponseBody(t *testing.T) { /* ... */ }

   // Bad - testing multiple things
   func TestEverything(t *testing.T) { /* ... */ }
   ```

4. **Use table-driven tests for multiple cases**
   ```go
   tests := []struct{...}{ /* test cases */ }
   ```

5. **Use `t.Helper()` in test helper functions**
   ```go
   func assertStatusOK(t *testing.T, code int) {
       t.Helper() // Errors point to caller, not this function
       if code != 200 {
           t.Errorf("Expected 200, got %d", code)
       }
   }
   ```

### ❌ DON'T

1. **Don't skip error checking**
   ```go
   // Bad
   req, _ := http.NewRequest("GET", "/", nil)

   // Good
   req, err := http.NewRequest("GET", "/", nil)
   if err != nil {
       t.Fatalf("Failed to create request: %v", err)
   }
   ```

2. **Don't use `panic()` in tests** - use `t.Fatal()` or `t.Error()`

3. **Don't depend on test execution order** - tests should be independent

4. **Don't test the framework** - test YOUR code
   ```go
   // Bad - testing Gin, not your code
   if router != nil { ... }

   // Good - testing your handler's behavior
   if rr.Body.String() != expected { ... }
   ```

5. **Don't forget to clean up** - use `defer` for teardown
   ```go
   db := setupDB()
   defer db.Close()
   ```

---

## Testing Checklist

Before committing code, ensure:

- [ ] All tests pass: `go test ./...`
- [ ] Code coverage is acceptable: `go test -cover`
- [ ] Tests are readable and well-named
- [ ] Edge cases are tested
- [ ] Error conditions are tested
- [ ] No print statements left in code (use `t.Log()` instead)

---

## Quick Reference

### Test Functions

| Function | Use Case |
|----------|----------|
| `t.Error()` / `t.Errorf()` | Report failure, continue test |
| `t.Fatal()` / `t.Fatalf()` | Report failure, stop test immediately |
| `t.Log()` / `t.Logf()` | Print message (only with `-v`) |
| `t.Skip()` / `t.Skipf()` | Skip test |
| `t.Run(name, func)` | Run subtest |
| `t.Helper()` | Mark function as test helper |

### HTTP Testing

| Function | Purpose |
|----------|---------|
| `httptest.NewRecorder()` | Create fake response writer |
| `http.NewRequest(method, path, body)` | Create fake request |
| `router.ServeHTTP(rr, req)` | Execute request |
| `rr.Code` | Get response status code |
| `rr.Body.String()` | Get response body as string |
| `rr.Header().Get("name")` | Get response header |

### Running Tests

```bash
go test              # Run tests
go test -v           # Verbose
go test -run Name    # Run specific test
go test -cover       # Coverage
go test -bench .     # Benchmarks
go test ./...        # All packages
```

---

## Next Steps

1. ✅ Write your first test (`main_test.go`)
2. ✅ Run the test: `go test -v`
3. ✅ Practice debugging with `t.Logf()`
4. ⏭️ Add tests for database layer (Issue #9)
5. ⏭️ Add tests for HTTP handlers (Issue #10)
6. ⏭️ Integrate tests into CI/CD pipeline

---

**End of Testing and Debugging Guide**

*Keep this document handy as you build your full-stack application!*
