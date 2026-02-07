# Bootstrap Integration Guide

**CS408 Spring 2026** | Tutorial for Adding UI Frameworks to Go Templates

---

## Table of Contents
1. [What is Bootstrap?](#what-is-bootstrap)
2. [Why Use a UI Framework?](#why-use-a-ui-framework)
3. [Adding Bootstrap via CDN](#adding-bootstrap-via-cdn)
4. [Using Bootstrap Classes](#using-bootstrap-classes)
5. [Testing Bootstrap Integration](#testing-bootstrap-integration)
6. [Common Bootstrap Components](#common-bootstrap-components)

---

## What is Bootstrap?

**Bootstrap** is the most popular CSS framework for building responsive, mobile-first websites.

**Key Features:**
- Pre-built CSS classes for styling
- Responsive grid system
- JavaScript components (modals, dropdowns, etc.)
- Consistent design across browsers
- Mobile-first approach

**Version:** Bootstrap 5.3 (latest as of 2026)

---

## Why Use a UI Framework?

### Without Bootstrap
```html
<h1 style="font-size: 48px; color: blue; text-align: center;">Hello World</h1>
<button style="background: green; color: white; padding: 10px 20px; border: none; border-radius: 5px;">
    Click Me
</button>
```
- ❌ Lots of inline styles
- ❌ Hard to maintain
- ❌ Not responsive
- ❌ Inconsistent across pages

### With Bootstrap
```html
<h1 class="display-1 text-primary text-center">Hello World</h1>
<button class="btn btn-success btn-lg">Click Me</button>
```
- ✅ Clean, readable code
- ✅ Responsive by default
- ✅ Consistent styling
- ✅ Easy to customize

---

## Adding Bootstrap via CDN

### Step 1: Update layout.html

**File:** `templates/layout.html`

```html
{{define "layout"}}
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>{{.Title}}</title>

    <!-- Bootstrap CSS CDN -->
    <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.3.0/dist/css/bootstrap.min.css"
          rel="stylesheet">
</head>
<body>
    <!-- Container class adds padding and centers content -->
    <div class="container mt-5">
        {{block "content" .}}{{end}}
    </div>

    <!-- Bootstrap JS Bundle (includes Popper) -->
    <script src="https://cdn.jsdelivr.net/npm/bootstrap@5.3.0/dist/js/bootstrap.bundle.min.js">
    </script>
</body>
</html>
{{end}}
```

### What Each Part Does

**Line 5: Viewport Meta Tag**
```html
<meta name="viewport" content="width=device-width, initial-scale=1">
```
- Makes your site responsive on mobile devices
- Required for Bootstrap to work properly

**Line 9-10: Bootstrap CSS CDN**
```html
<link href="https://cdn.jsdelivr.net/npm/bootstrap@5.3.0/dist/css/bootstrap.min.css"
      rel="stylesheet">
```
- Loads Bootstrap CSS from a CDN (Content Delivery Network)
- Fast, cached, no download needed
- Alternative: Download and serve locally from `/static/css/`

**Line 14: Container Class**
```html
<div class="container mt-5">
```
- `container` - Centers content with responsive padding
- `mt-5` - Adds margin-top (spacing from top of page)

**Line 19-20: Bootstrap JS Bundle**
```html
<script src="https://cdn.jsdelivr.net/npm/bootstrap@5.3.0/dist/js/bootstrap.bundle.min.js">
</script>
```
- Enables interactive components (modals, dropdowns, tooltips)
- Includes Popper.js for positioning
- Place at end of `<body>` for faster page load

---

## Using Bootstrap Classes

### Step 2: Update index.html with Bootstrap Classes

**File:** `templates/index.html`

```html
{{define "content"}}
<div class="text-center">
    <h1 class="display-1 text-primary mb-4">{{.Title}}</h1>
    <p class="lead mb-4">Welcome to my Go full-stack web application!</p>
    <button class="btn btn-success btn-lg">Click Me!</button>
</div>
{{end}}
```

### Bootstrap Class Breakdown

| Class | Purpose | Effect |
|-------|---------|--------|
| `text-center` | Text alignment | Centers all text inside the div |
| `display-1` | Typography | Extra large heading (responsive) |
| `text-primary` | Color | Blue color (Bootstrap's primary color) |
| `mb-4` | Spacing | Margin-bottom: 1.5rem |
| `lead` | Typography | Larger paragraph text |
| `btn` | Button base | Makes element look like a button |
| `btn-success` | Button color | Green button |
| `btn-lg` | Button size | Large button |

---

## Testing Bootstrap Integration

### Visual Test: Run Your App

**1. Start the server:**
```bash
go run .
```

**2. Open browser:**
```
http://localhost:3000
```

**3. What you should see:**
- ✅ Large blue "Hello World" heading
- ✅ Centered content
- ✅ Large green button
- ✅ Professional spacing and typography

**4. Test responsive design:**
- Resize browser window (make it narrow)
- Text should remain readable and centered
- Button should adjust size

### Automated Test: Verify Bootstrap is Loaded

**Add to `main_test.go`:**

```go
// TestBootstrapLoaded checks if Bootstrap CSS is included
func TestBootstrapLoaded(t *testing.T) {
	templates := make(map[string]*template.Template)
	templates["index"] = template.Must(template.ParseFiles(
		"templates/layout.html",
		"templates/index.html",
	))

	gin.SetMode(gin.TestMode)
	router := gin.Default()

	router.GET("/", func(c *gin.Context) {
		c.Writer.WriteHeader(200)
		templates["index"].ExecuteTemplate(c.Writer, "layout", gin.H{
			"Title": "Hello World",
		})
	})

	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// Check that Bootstrap CDN link is in the response
	if !strings.Contains(rr.Body.String(), "bootstrap") {
		t.Error("Bootstrap CSS not found in response")
	}

	// Check that Bootstrap classes are present
	if !strings.Contains(rr.Body.String(), "class=\"container") {
		t.Error("Bootstrap container class not found")
	}

	t.Log("✅ Bootstrap is properly integrated!")
}
```

**Run the test:**
```bash
go test -v -run TestBootstrapLoaded
```

---

## Common Bootstrap Components

### Buttons

```html
<!-- Different colors -->
<button class="btn btn-primary">Primary</button>
<button class="btn btn-secondary">Secondary</button>
<button class="btn btn-success">Success</button>
<button class="btn btn-danger">Danger</button>
<button class="btn btn-warning">Warning</button>
<button class="btn btn-info">Info</button>

<!-- Different sizes -->
<button class="btn btn-primary btn-sm">Small</button>
<button class="btn btn-primary">Default</button>
<button class="btn btn-primary btn-lg">Large</button>

<!-- Outline buttons -->
<button class="btn btn-outline-primary">Outline</button>
```

### Typography

```html
<!-- Headings -->
<h1 class="display-1">Display 1</h1>  <!-- Largest -->
<h1 class="display-4">Display 4</h1>  <!-- Smaller -->

<!-- Lead paragraph -->
<p class="lead">This is a lead paragraph - larger text.</p>

<!-- Text colors -->
<p class="text-primary">Blue text</p>
<p class="text-success">Green text</p>
<p class="text-danger">Red text</p>
<p class="text-muted">Gray text</p>

<!-- Text alignment -->
<p class="text-start">Left aligned</p>
<p class="text-center">Center aligned</p>
<p class="text-end">Right aligned</p>
```

### Cards

```html
<div class="card" style="width: 18rem;">
  <div class="card-body">
    <h5 class="card-title">Card Title</h5>
    <p class="card-text">Some quick example text.</p>
    <a href="#" class="btn btn-primary">Go somewhere</a>
  </div>
</div>
```

### Forms

```html
<form>
  <div class="mb-3">
    <label for="email" class="form-label">Email address</label>
    <input type="email" class="form-control" id="email">
  </div>
  <div class="mb-3">
    <label for="password" class="form-label">Password</label>
    <input type="password" class="form-control" id="password">
  </div>
  <button type="submit" class="btn btn-primary">Submit</button>
</form>
```

### Grid System

```html
<div class="container">
  <div class="row">
    <div class="col-md-6">
      <!-- Left column (50% on medium+ screens) -->
    </div>
    <div class="col-md-6">
      <!-- Right column (50% on medium+ screens) -->
    </div>
  </div>
</div>
```

### Alerts

```html
<div class="alert alert-success" role="alert">
  Success! Your action was completed.
</div>

<div class="alert alert-danger" role="alert">
  Error! Something went wrong.
</div>
```

### Navigation

```html
<nav class="navbar navbar-expand-lg navbar-dark bg-dark">
  <div class="container-fluid">
    <a class="navbar-brand" href="/">My App</a>
    <div class="navbar-nav">
      <a class="nav-link" href="/">Home</a>
      <a class="nav-link" href="/about">About</a>
    </div>
  </div>
</nav>
```

---

## Spacing Utilities

Bootstrap uses a consistent spacing scale:

### Margin (m) and Padding (p)

| Class | Spacing |
|-------|---------|
| `m-0` / `p-0` | 0 |
| `m-1` / `p-1` | 0.25rem |
| `m-2` / `p-2` | 0.5rem |
| `m-3` / `p-3` | 1rem |
| `m-4` / `p-4` | 1.5rem |
| `m-5` / `p-5` | 3rem |

### Directions

| Class | Side |
|-------|------|
| `mt-3` | Margin-top |
| `mb-3` | Margin-bottom |
| `ms-3` | Margin-start (left) |
| `me-3` | Margin-end (right) |
| `mx-3` | Margin horizontal (left + right) |
| `my-3` | Margin vertical (top + bottom) |

**Examples:**
```html
<div class="mt-5">Margin top</div>
<div class="mb-3">Margin bottom</div>
<div class="p-4">Padding all sides</div>
<div class="px-5">Padding left and right</div>
```

---

## Responsive Design

Bootstrap uses breakpoints for responsive design:

| Breakpoint | Screen Width | Class Prefix |
|------------|--------------|--------------|
| Extra small | <576px | (none) |
| Small | ≥576px | `sm` |
| Medium | ≥768px | `md` |
| Large | ≥992px | `lg` |
| Extra large | ≥1200px | `xl` |
| Extra extra large | ≥1400px | `xxl` |

**Example: Responsive columns**
```html
<div class="row">
  <div class="col-12 col-md-6 col-lg-4">
    <!-- 100% width on mobile, 50% on tablet, 33% on desktop -->
  </div>
</div>
```

---

## Customizing Bootstrap

### Option 1: Override with Custom CSS

Create `static/stylesheets/custom.css`:

```css
/* Custom colors */
.btn-custom {
    background-color: #ff6b6b;
    color: white;
}

/* Override Bootstrap primary color */
.text-primary {
    color: #your-brand-color !important;
}
```

**Load after Bootstrap in layout.html:**
```html
<link href="https://cdn.jsdelivr.net/npm/bootstrap@5.3.0/dist/css/bootstrap.min.css"
      rel="stylesheet">
<link href="/stylesheets/custom.css" rel="stylesheet">
```

### Option 2: Use Bootstrap Themes

- [Bootswatch](https://bootswatch.com/) - Free themes
- [Bootstrap Themes](https://themes.getbootstrap.com/) - Official themes

---

## Best Practices

### ✅ DO

1. **Use semantic HTML with Bootstrap classes**
   ```html
   <button class="btn btn-primary">Submit</button>
   <!-- Not: <div class="btn">Submit</div> -->
   ```

2. **Use container for page structure**
   ```html
   <div class="container">
     <!-- Your content -->
   </div>
   ```

3. **Use spacing utilities instead of custom CSS**
   ```html
   <h1 class="mb-4">Title</h1>
   <!-- Not: <h1 style="margin-bottom: 1.5rem">Title</h1> -->
   ```

4. **Test on different screen sizes**
   - Use browser dev tools responsive mode
   - Test on actual mobile devices

### ❌ DON'T

1. **Don't mix too many custom styles**
   - Use Bootstrap utilities first
   - Only add custom CSS when necessary

2. **Don't forget viewport meta tag**
   ```html
   <meta name="viewport" content="width=device-width, initial-scale=1">
   ```

3. **Don't load Bootstrap multiple times**
   - Load once in layout.html
   - Don't load in individual pages

4. **Don't use outdated Bootstrap versions**
   - Bootstrap 5 is current (2026)
   - Bootstrap 3/4 syntax is different

---

## Debugging Bootstrap Issues

### Issue: Styles Not Showing

**Check:**
1. Is Bootstrap CDN link in `<head>`?
2. Is viewport meta tag present?
3. Are class names spelled correctly?
4. Open browser dev tools → Network → Check if bootstrap.min.css loaded (200 OK)

**Test:**
```bash
curl -I https://cdn.jsdelivr.net/npm/bootstrap@5.3.0/dist/css/bootstrap.min.css
```

### Issue: JavaScript Components Not Working

**Check:**
1. Is Bootstrap JS loaded before `</body>`?
2. Are data attributes correct? (e.g., `data-bs-toggle="modal"`)
3. Open browser console for JavaScript errors

### Issue: Responsive Not Working

**Check:**
1. Viewport meta tag present?
2. Using correct responsive classes? (col-md-6, etc.)
3. Test in browser responsive mode

---

## Resources

**Official Documentation:**
- [Bootstrap Docs](https://getbootstrap.com/docs/5.3/getting-started/introduction/)
- [Bootstrap Examples](https://getbootstrap.com/docs/5.3/examples/)

**Learning:**
- [Bootstrap 5 Crash Course](https://www.youtube.com/watch?v=4sosXZsdy-s)
- [W3Schools Bootstrap 5 Tutorial](https://www.w3schools.com/bootstrap5/)

**Tools:**
- [Bootstrap Icons](https://icons.getbootstrap.com/)
- [Bootstrap Cheatsheet](https://bootstrap-cheatsheet.themeselection.com/)

---

## Summary

You learned:
- ✅ What Bootstrap is and why to use it
- ✅ How to add Bootstrap via CDN
- ✅ Common Bootstrap classes and components
- ✅ Responsive design with Bootstrap
- ✅ How to test Bootstrap integration
- ✅ Debugging Bootstrap issues

**Next Steps:**
- Explore Bootstrap components
- Build a navigation bar
- Create a todo list UI with Bootstrap cards
- Customize Bootstrap with your own styles

---

**End of Bootstrap Integration Guide**
