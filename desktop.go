package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"rohy/backend/api"
)

// desktop is api.Desktop over Wails: the only place rohy calls the Wails runtime, so the bindings
// and their tests never import it.
//
// Its fields are set once in main, before the app runs, and only read after.
type desktop struct {
	app *application.App
	// window owns every dialog, so a dialog is modal to it as it was under Wails v2.
	window application.Window
}

// errDialogCancelled is the text of the error Wails' Windows dialogs return when the analyst
// cancels. The sentinel itself is in an internal package, so its text is matched here, and a test
// pins it to the Wails source.
const errDialogCancelled = "cancelled by user"

// picked turns a cancelled dialog into no selection, as on every other platform.
func picked[T any](v T, err error) (T, error) {
	if err != nil && err.Error() == errDialogCancelled {
		var none T
		return none, nil
	}
	return v, err
}

func (d *desktop) Emit(channel string, data any) {
	d.app.Event.Emit(channel, data)
}

func (d *desktop) PickFiles(title string, filter api.FileFilter) ([]string, error) {
	return picked(d.app.Dialog.OpenFile().
		AttachToWindow(d.window).
		SetTitle(title).
		AddFilter(filter.Name, filter.Pattern).
		CanChooseFiles(true).
		PromptForMultipleSelection())
}

func (d *desktop) PickFolder(title string) (string, error) {
	return picked(d.app.Dialog.OpenFile().
		AttachToWindow(d.window).
		SetTitle(title).
		CanChooseFiles(false).
		CanChooseDirectories(true).
		PromptForSingleSelection())
}

func (d *desktop) PickFile(title string, filter api.FileFilter) (string, error) {
	return picked(d.app.Dialog.OpenFile().
		AttachToWindow(d.window).
		SetTitle(title).
		AddFilter(filter.Name, filter.Pattern).
		CanChooseFiles(true).
		PromptForSingleSelection())
}

func (d *desktop) PickNewFile(title, name string, filter api.FileFilter) (string, error) {
	// The save dialog takes its title only through its options.
	return picked(d.app.Dialog.SaveFileWithOptions(&application.SaveFileDialogOptions{
		Title:                title,
		Filename:             name,
		Filters:              []application.FileFilter{{DisplayName: filter.Name, Pattern: filter.Pattern}},
		CanCreateDirectories: true,
		Window:               d.window,
	}).PromptForSingleSelection())
}

func (d *desktop) Quit() {
	d.app.Quit()
}
