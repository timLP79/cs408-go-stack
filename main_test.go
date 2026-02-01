package main

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestIndexRoute tests the main index route
func TestIndexRoute(t *testing.T) {
	templates := make(map[string]*template.Template)
	templates["index"] = template.Must(template.ParseFiles(
		"templates/layout.html",
		"templates/index.html"))

	gin.SetMode(gin.TestMode)
	router := gin.Default()

	router.GET("/", func(c *gin.Context) {
		c.Writer.WriteHeader(200)
		templates["index"].ExecuteTemplate(c.Writer, "layout", gin.H{
			"Title": "Hello World",
		})
	})

	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	/*// DEBUG: let's see what is in the body
	t.Logf("========== DEBUGGING ==========")
	t.Logf("Status Code: %d", rr.Code)
	t.Logf("\n========== FULL RESPONSE BODY ==========")
	t.Logf("%s", rr.Body.String())
	t.Logf("========== END RESPONSE ==========\n")*/

	// check status 200 is OK
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	expectedText := "Hello World"
	if !strings.Contains(rr.Body.String(), expectedText) {
		t.Errorf("Handler returned unexpected body: expected to contain %q",
			expectedText)
	}

	// Success
	t.Log("Index route test passed!")
}
