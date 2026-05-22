// Copyright (c) 2026 Tim Palacios. All rights reserved.
// Licensed under the LibreShelf License (see LICENSE in the repo root).

package main

import (
	"database/sql"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// overdueGroup is the per-patron grouping rendered on the /reports/overdue
// table: one row per patron with their overdue loans nested under it.
type overdueGroup struct {
	PatronID   int
	PatronName string
	Loans      []LoanListRow
}

func HandleReportsOverdue(c *gin.Context) {
	dm := getDB(c)
	loans, err := dm.GetOverdueLoans()
	if err != nil {
		log.Printf("HandleReportsOverdue: GetOverdueLoans: %v", err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}

	// Group by patron. GetOverdueLoans orders by due_date; preserve that
	// order within each patron group by appending in arrival order.
	groupsByID := map[int]*overdueGroup{}
	var order []int
	for _, ln := range loans {
		g, ok := groupsByID[ln.PatronID]
		if !ok {
			g = &overdueGroup{PatronID: ln.PatronID, PatronName: ln.PatronName}
			groupsByID[ln.PatronID] = g
			order = append(order, ln.PatronID)
		}
		g.Loans = append(g.Loans, ln)
	}
	groups := make([]*overdueGroup, 0, len(order))
	for _, id := range order {
		groups = append(groups, groupsByID[id])
	}

	renderTemplate(c, "reports_overdue", gin.H{
		"Title":      "Overdue Report",
		"Groups":     groups,
		"TotalLoans": len(loans),
	})
}

func HandleOverdueNotice(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		HandleNotFound(c)
		return
	}

	dm := getDB(c)
	patron, err := dm.GetPatronByID(id)
	if err == sql.ErrNoRows {
		HandleNotFound(c)
		return
	}
	if err != nil {
		log.Printf("HandleOverdueNotice: GetPatronByID: %v", err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}

	loans, err := dm.GetPatronOverdueLoansForNotice(id)
	if err != nil {
		log.Printf("HandleOverdueNotice: GetPatronOverdueLoansForNotice: %v", err)
		c.String(http.StatusInternalServerError, "Internal Server Error")
		return
	}
	if len(loans) == 0 {
		// No notice to print -- treat as not-found rather than render an
		// empty notice that would confuse the admin (and waste paper).
		HandleNotFound(c)
		return
	}

	renderTemplate(c, "overdue_notice", gin.H{
		"Title":      "Overdue Notice",
		"Patron":     patron,
		"Loans":      loans,
		"NoticeDate": time.Now().Format("January 2, 2006"),
	})
}
