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
