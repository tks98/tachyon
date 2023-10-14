package main

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Fetch container information and cache it before the application starts
func init() {
	StartCacheRefresh()
}

func main() {
	// Init the TUI
	app := tview.NewApplication()

	// Create a Flex layout for a split screen
	flex := tview.NewFlex().SetDirection(tview.FlexRow)

	// Configure the main layout
	mainLayout := tview.NewFlex()

	// Create a table for the main view with fixed headers
	table := createAppTable(app)

	// Set up detailed view of the container
	detailsTextView := createDetailsTextview(app, table)

	// Select the first container row, refresh the table view, and update container details
	table.Select(1, 0)
	refreshTable(table, detailsTextView)
	updateDetails(table, detailsTextView)

	// Configure input capture logic for the TUI
	// Hitting right arrow key moves to container details view
	// Hitting left arrow key moves back to container table view
	// Hitting up or down arrow keys either moves to next container in the table
	// or scrolls in the container details view
	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		currentRow, _ := table.GetSelection()
		switch event.Key() {
		case tcell.KeyRight:
			app.SetFocus(detailsTextView)
			return event
		case tcell.KeyLeft:
			app.SetFocus(table)
			return event
		case tcell.KeyUp:
			if currentRow > 1 { // Starting from 1 to avoid table headers.
				table.Select(currentRow-1, 0)
				updateDetails(table, detailsTextView)
			}
			return nil // Prevent the default behavior of the table.
		case tcell.KeyDown:
			if currentRow < table.GetRowCount()-1 {
				table.Select(currentRow+1, 0)
				updateDetails(table, detailsTextView)
			}
			return nil // Prevent the default behavior of the table.
		}
		return event
	})

	// Add main elements to the main layout
	mainLayout.AddItem(table, 0, 1, true)
	mainLayout.AddItem(detailsTextView, 0, 2, false)

	// Add the main layout to the flex layout
	flex.AddItem(mainLayout, 0, 10, true)

	// Set up app-wide shortcuts
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Rune() {
		case 'r': // Refresh table
			refreshTable(table, detailsTextView)
		case 'q': // Quit the application
			app.Stop()
		}
		return event
	})

	// Start the application
	if err := app.SetRoot(flex, true).EnableMouse(true).Run(); err != nil {
		panic(err)
	}
}
