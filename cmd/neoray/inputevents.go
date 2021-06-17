package main

import (
	"strings"

	"github.com/go-gl/glfw/v3.3/glfw"
)

var (
	SpecialKeys = map[glfw.Key]string{
		glfw.KeyEscape:    "ESC",
		glfw.KeyEnter:     "CR",
		glfw.KeyKPEnter:   "kEnter",
		glfw.KeySpace:     "Space",
		glfw.KeyBackspace: "BS",
		glfw.KeyUp:        "Up",
		glfw.KeyDown:      "Down",
		glfw.KeyRight:     "Right",
		glfw.KeyLeft:      "Left",
		glfw.KeyTab:       "Tab",
		glfw.KeyInsert:    "Insert",
		glfw.KeyDelete:    "Del",
		glfw.KeyHome:      "Home",
		glfw.KeyEnd:       "End",
		glfw.KeyPageUp:    "PageUp",
		glfw.KeyPageDown:  "PageDown",
		glfw.KeyF1:        "F1",
		glfw.KeyF2:        "F2",
		glfw.KeyF3:        "F3",
		glfw.KeyF4:        "F4",
		glfw.KeyF5:        "F5",
		glfw.KeyF6:        "F6",
		glfw.KeyF7:        "F7",
		glfw.KeyF8:        "F8",
		glfw.KeyF9:        "F9",
		glfw.KeyF10:       "F10",
		glfw.KeyF11:       "F11",
		glfw.KeyF12:       "F12",
	}

	ModifierKeys = map[glfw.Key]string{
		glfw.KeyLeftControl:  "C",
		glfw.KeyRightControl: "C",
		glfw.KeyLeftAlt:      "A",
		glfw.KeyRightAlt:     "A",
		glfw.KeyLeftShift:    "S",
		glfw.KeyRightShift:   "S",
		glfw.KeyLeftSuper:    "D",
		glfw.KeyRightSuper:   "D",
	}

	// Last mouse informations
	lastMousePos       IntVec2
	lastMouseButton    string
	lastMouseModifiers string
	lastMouseAction    glfw.Action

	// Options
	zoomInKey           string
	zoomOutKey          string
	toggleFullscreenKey string
	popupMenuEnabled    bool
)

func InitializeInputEvents() {
	// Initialize defaults
	zoomInKey = "<C-+>"
	zoomOutKey = "<C-->"
	toggleFullscreenKey = "<F11>"
	popupMenuEnabled = true
	// Initialize callbacks
	EditorSingleton.window.handle.SetCharModsCallback(CharEventHandler)
	EditorSingleton.window.handle.SetKeyCallback(KeyEventHandler)
	EditorSingleton.window.handle.SetMouseButtonCallback(ButtonEventHandler)
	EditorSingleton.window.handle.SetCursorPosCallback(MousePosEventHandler)
	EditorSingleton.window.handle.SetScrollCallback(ScrollEventHandler)
	EditorSingleton.window.handle.SetDropCallback(DropEventHandler)
}

func CharEventHandler(w *glfw.Window, char rune, mods glfw.ModifierKey) {
	var keycode string
	c := string(char)
	switch c {
	// These characters are handled in Key callback
	case " ":
		return
	// This is special character
	case "<":
		keycode = "<LT>"
	default:
		keycode = c
	}
	EditorSingleton.nvim.Input(keycode)
}

func KeyEventHandler(w *glfw.Window, key glfw.Key, scancode int, action glfw.Action, mods glfw.ModifierKey) {
	// If this is a modifier key, we will store it if it it pressed,
	// delete it if it is released
	code, ok := ModifierKeys[key]
	if ok {
		if action == glfw.Press {
			if strings.Index(lastMouseModifiers, code) == -1 {
				lastMouseModifiers += code
			}
		} else if action == glfw.Release && lastMouseModifiers != "" {
			lastMouseModifiers = strings.Replace(lastMouseModifiers, code, "", 1)
		}
		return
	}

	if action != glfw.Release {
		ctrl := mods&glfw.ModControl != 0
		shift := mods&glfw.ModShift != 0
		alt := mods&glfw.ModAlt != 0
		super := mods&glfw.ModSuper != 0
		var keyname string
		name, ok := SpecialKeys[key]
		if ok {
			keyname = name
		} else {
			if shift || alt || super {
				return
			} else if ctrl {
				keyname = glfw.GetKeyName(key, scancode)
				if keyname == "" {
					return
				}
			}
		}
		keycode := "<"
		if ctrl {
			keycode += "C-"
		}
		if shift {
			keycode += "S-"
		}
		if alt {
			keycode += "A-"
		}
		if super {
			keycode += "D-"
		}
		keycode += keyname + ">"

		// Neoray keybindings are there.
		switch keycode {
		case zoomInKey:
			EditorSingleton.renderer.IncreaseFontSize()
			return
		case zoomOutKey:
			EditorSingleton.renderer.DecreaseFontSize()
			return
		case toggleFullscreenKey:
			EditorSingleton.window.ToggleFullscreen()
			return
		case "<ESC>":
			if popupMenuEnabled {
				EditorSingleton.popupMenu.Hide()
			}
			break
		default:
			break
		}

		EditorSingleton.nvim.Input(keycode)
	}
}

func ButtonEventHandler(w *glfw.Window, button glfw.MouseButton, action glfw.Action, mods glfw.ModifierKey) {
	var buttonCode string
	switch button {
	case glfw.MouseButtonLeft:
		if action == glfw.Press && popupMenuEnabled {
			if EditorSingleton.popupMenu.MouseClick(false, lastMousePos) {
				return
			}
		}
		buttonCode = "left"
		break
	case glfw.MouseButtonRight:
		if action == glfw.Press && popupMenuEnabled {
			// We don't send right button to neovim if popup menu enabled.
			EditorSingleton.popupMenu.MouseClick(true, lastMousePos)
			return
		}
		buttonCode = "right"
		break
	case glfw.MouseButtonMiddle:
		buttonCode = "middle"
		break
	default:
		return
	}

	actionCode := "press"
	if action == glfw.Release {
		actionCode = "release"
	}

	row := lastMousePos.Y / EditorSingleton.cellHeight
	col := lastMousePos.X / EditorSingleton.cellWidth
	EditorSingleton.nvim.InputMouse(buttonCode, actionCode, lastMouseModifiers, 0, row, col)

	lastMouseButton = buttonCode
	lastMouseAction = action
}

func MousePosEventHandler(w *glfw.Window, xpos, ypos float64) {
	lastMousePos.X = int(xpos)
	lastMousePos.Y = int(ypos)
	if popupMenuEnabled {
		EditorSingleton.popupMenu.MouseMove(lastMousePos)
	}
	// If mouse moving when holding left button, it's drag event
	if lastMouseAction == glfw.Press {
		row := lastMousePos.Y / EditorSingleton.cellHeight
		col := lastMousePos.X / EditorSingleton.cellWidth
		EditorSingleton.nvim.InputMouse(lastMouseButton, "drag", lastMouseModifiers, 0, row, col)
	}
}

func ScrollEventHandler(w *glfw.Window, xpos, ypos float64) {
	action := "up"
	if ypos < 0 {
		action = "down"
	}
	row := lastMousePos.Y / EditorSingleton.cellHeight
	col := lastMousePos.X / EditorSingleton.cellWidth
	EditorSingleton.nvim.InputMouse("wheel", action, lastMouseModifiers, 0, row, col)
}

func DropEventHandler(w *glfw.Window, names []string) {
	for _, name := range names {
		EditorSingleton.nvim.OpenFile(name)
	}
}
