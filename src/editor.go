package main

import (
	"bytes"
	"image"
	"image/png"
	"time"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/hismailbulut/neoray/src/assets"
	"github.com/hismailbulut/neoray/src/bench"
	"github.com/hismailbulut/neoray/src/common"
	"github.com/hismailbulut/neoray/src/fontkit"
	"github.com/hismailbulut/neoray/src/logger"
	"github.com/hismailbulut/neoray/src/window"
)

type Options struct {
	// custom options
	cursorAnimTime      float32
	transparency        float32
	targetTPS           int
	contextMenuEnabled  bool
	boxDrawingEnabled   bool
	keyToggleFullscreen string
	keyIncreaseFontSize string
	keyDecreaseFontSize string
}

func DefaultOptions() Options {
	return Options{
		cursorAnimTime:      0.1,
		transparency:        1,
		targetTPS:           60,
		contextMenuEnabled:  true,
		boxDrawingEnabled:   true,
		keyToggleFullscreen: "<F11>",
		keyIncreaseFontSize: "<C-kPlus>",
		keyDecreaseFontSize: "<C-kMinus>",
	}
}

type EditorState uint32

const (
	EditorNotInitialized EditorState = iota
	EditorInitialized
	EditorLoopStarted // Mainloop started and app running, but not everything ready because of neovim events processed at mainloop
	EditorFirstFlush  // This is where we start to check NeoraySet options
	EditorWindowShown // We show window after first NeoraySet option check
	EditorLoopStopped
	EditorDestroyed
)

var Editor struct {
	state EditorState
	// Parsed startup arguments
	parsedArgs ParsedArgs
	// IPC server for singleinstance
	server *IpcServer
	// Neoray options.
	options Options
	// Main window of this program.
	window *window.Window
	// Grid manager holds information about neovim grids and how they will be rendered
	// We also use its underlying rendering structure when rendering cursor and context menu
	gridManager *GridManager
	// Cursor represents a neovim cursor and all it's information
	cursor *Cursor
	// ContextMenu is the only context menu in this program for right click menu.
	contextMenu *ContextMenu
	// UIOptions is a struct, holds some user ui uiOptions like guifont.
	uiOptions UIOptions
	// Neovim child process
	nvim *NvimProcess
	// MainLoop ticker
	ticker *time.Ticker
	// Stops mainloop
	quitChan chan bool
	// Draw calls
	cDraw      bool
	cForceDraw bool
	cRender    bool
}

func InitEditor() {
	var err error

	Editor.options = DefaultOptions()

	err = glfw.Init()
	if err != nil {
		logger.Log(logger.FATAL, "Failed to initialize GLFW3:", err)
	}
	logger.Log(logger.TRACE, "GLFW3 Version:", glfw.GetVersionString())

	Editor.window, err = window.New(NAME, 800, 600, bench.IsDebugBuild())
	if err != nil {
		logger.Log(logger.FATAL, err)
	}
	// Event handler function runs when we call window.PollEvents
	Editor.window.SetEventHandler(EventHandler)
	// Set window minimum size
	Editor.window.SetMinSize(common.Vec2(300, 200))
	// Set window icons
	LoadDefaultIcons()
	// Update opengl viewport
	Editor.window.GL().SetViewport(Editor.window.Viewport())
	// Print some opengl info
	info := Editor.window.GL().Info()
	logger.Log(logger.TRACE, "Opengl Version:", info.Version)
	logger.Log(logger.TRACE, "Vendor:", info.Vendor)
	logger.Log(logger.TRACE, "Renderer:", info.Renderer)
	logger.Log(logger.TRACE, "GLSL:", info.ShadingLanguageVersion)
	logger.Log(logger.TRACE, "Max Texture Size:", info.MaxTextureSize)
	// Initialize gridManager
	Editor.gridManager = NewGridManager()
	// Initialize cursor
	Editor.cursor = NewCursor(Editor.window)
	// Initialize contextMenu
	Editor.contextMenu = NewContextMenu()
	// TODO Move this to gridManager
	Editor.uiOptions = CreateUIOptions()
	// Start neovim
	Editor.nvim = CreateNvimProcess()
	// Calculate temporary start size and start the ui connection
	// The size will be updated according to user preferences
	cellSize := DefaultCellSize()
	cols := Editor.window.Size().Width() / cellSize.Width()
	rows := Editor.window.Size().Height() / cellSize.Height()
	logger.Log(logger.DEBUG, "Calculated startup size of the neovim is", rows, cols)
	Editor.nvim.StartUI(rows, cols)

	Editor.quitChan = make(chan bool, 1)

	SetEditorState(EditorInitialized)
}

func LoadDefaultIcons() {
	icons := [3]image.Image{}
	icon48, err := png.Decode(bytes.NewReader(assets.NeovimIconData48x48))
	if err != nil {
		logger.Log(logger.ERROR, "Failed to decode 48x48 icon:", err)
	} else {
		icons[0] = icon48
	}

	icon32, err := png.Decode(bytes.NewReader(assets.NeovimIconData32x32))
	if err != nil {
		logger.Log(logger.ERROR, "Failed to decode 32x32 icon:", err)
	} else {
		icons[1] = icon32
	}

	icon16, err := png.Decode(bytes.NewReader(assets.NeovimIconData16x16))
	if err != nil {
		logger.Log(logger.ERROR, "Failed to decode 16x16 icon:", err)
	} else {
		icons[2] = icon16
	}
	Editor.window.SetIcon(icons)
}

// A helper function, if default grid is not set by neovim yet we use this for cell size
func DefaultCellSize() common.Vector2[int] {
	face, _ := fontkit.Default().DefaultFont().CreateFace(fontkit.FaceParams{
		Size:            DEFAULT_FONT_SIZE,
		DPI:             Editor.window.DPI(),
		UseBoxDrawing:   false,
		UseBlockDrawing: false,
	})
	return face.ImageSize()
}

func ResizeWindowInCellFormat(rows, cols int) {
	var size common.Vector2[int]
	defaultGrid := Editor.gridManager.Grid(1)
	if defaultGrid != nil {
		size.X = cols * defaultGrid.CellSize().Width()
		size.Y = rows * defaultGrid.CellSize().Height()
	} else {
		cellSize := DefaultCellSize()
		size.X = cols * cellSize.Width()
		size.Y = rows * cellSize.Height()
	}
	Editor.window.Resize(size)
}

// This is for making sure the state changing valid
func SetEditorState(state EditorState) {
	// assert(state-1 == Editor.state, "Editor state can only incremented by 1")
	Editor.state = state
}

func ResetTicker() {
	if Editor.ticker == nil {
		Editor.ticker = time.NewTicker(time.Second / time.Duration(Editor.options.targetTPS))
	} else {
		Editor.ticker.Reset(time.Second / time.Duration(Editor.options.targetTPS))
	}
}

func MarkDraw() {
	Editor.cDraw = true
}

func MarkForceDraw() {
	Editor.cForceDraw = true
}

func MarkRender() {
	Editor.cRender = true
}

func MainLoop() {
	SetEditorState(EditorLoopStarted)
	ResetTicker()
	// For measuring total time of the program.
	programBegin := time.Now()
	// For measuring ticks per second, debugging purposes
	upsTimer := 0.0
	updates := 0
	// For measuring elpased time
	lastTick := time.Now()
	// Mainloop
	run := true
	for run {
		select {
		case tick := <-Editor.ticker.C:
			// Calculate delta time
			elapsed := tick.Sub(lastTick)
			lastTick = tick
			delta := elapsed.Seconds()
			// Increment counters
			upsTimer += delta
			updates++
			// Calculate updates per second
			if upsTimer >= 1 {
				// println("TPS:", updates)
				updates = 0
				upsTimer -= 1
			}
			// Handle with inputs first
			Editor.window.PollEvents()
			// then update
			UpdateHandler(float32(delta))
		case <-Editor.quitChan:
			run = false
		}
	}
	SetEditorState(EditorLoopStopped)
	logger.Log(logger.TRACE, "Program finished. Total execution time:", time.Since(programBegin))
}

func UpdateHandler(delta float32) {
	// Update required stuff
	Editor.nvim.Update()
	Editor.gridManager.Update()
	Editor.cursor.Update(delta)
	if Editor.server != nil {
		Editor.server.Update()
	}
	// Draw calls
	if Editor.state >= EditorWindowShown {
		if Editor.cDraw || Editor.cForceDraw {
			EndBenchmark := bench.BeginBenchmark()
			Editor.gridManager.Draw(Editor.cForceDraw)
			Editor.cursor.Draw(delta)
			Editor.contextMenu.Draw()
			EndBenchmark("UpdateHandler.Draw")
		}
		// Render calls
		if Editor.cDraw || Editor.cForceDraw || Editor.cRender {
			EndBenchmark := bench.BeginBenchmark()
			Editor.window.GL().ClearScreen(Editor.gridManager.background.ToF32())
			Editor.gridManager.Render()
			Editor.cursor.Render()
			Editor.contextMenu.Render()
			Editor.window.GL().Flush()
			EndBenchmark("UpdateHandler.Render")
		}
		// Clear calls
		Editor.cDraw = false
		Editor.cForceDraw = false
		Editor.cRender = false
	}
}

func EventHandler(event window.WindowEvent) {
	switch event.Type {
	case window.WindowEventRefresh:
		{
			// Eg. When user resizing the window, glfw.PollEvents call is blocked.
			// And no events receives except this one. We need to update Neoray
			// additionally when refresh event received.
			EventHandler(window.WindowEvent{
				Type:   window.WindowEventResize,
				Params: []any{Editor.window.Size().Width(), Editor.window.Size().Height()},
			})
			// Pass delta as zero because this is an additional update
			UpdateHandler(0)
		}
	case window.WindowEventResize:
		{
			// Check grids sizes
			width := event.Params[0].(int)
			height := event.Params[1].(int)
			// When window minimized, glfw sends a resize event with zero size
			if width > 0 && height > 0 {
				// Try to resize the neovim
				defaultGrid := Editor.gridManager.Grid(1)
				if defaultGrid != nil {
					cellSize := defaultGrid.CellSize()
					rows := height / cellSize.Height()
					cols := width / cellSize.Width()
					if rows != defaultGrid.rows || cols != defaultGrid.cols {
						go Editor.nvim.tryResizeUI(rows, cols)
					}
				}
				// Update viewport
				Editor.window.GL().SetViewport(Editor.window.Viewport())
				// Render because viewport changed
				MarkRender()
			}
		}
	case window.WindowEventKeyInput:
		{
			key := event.Params[0].(glfw.Key)
			scancode := event.Params[1].(int)
			action := event.Params[2].(glfw.Action)
			mods := event.Params[3].(glfw.ModifierKey)
			KeyInputHandler(key, scancode, action, mods)
		}
	case window.WindowEventCharInput:
		{
			char := event.Params[0].(rune)
			CharInputHandler(char)
		}
	case window.WindowEventMouseInput:
		{
			button := event.Params[0].(glfw.MouseButton)
			action := event.Params[1].(glfw.Action)
			mods := event.Params[2].(glfw.ModifierKey)
			MouseInputHandler(button, action, mods)
		}
	case window.WindowEventMouseMove:
		{
			xpos := event.Params[0].(float64)
			ypos := event.Params[1].(float64)
			MouseMoveHandler(xpos, ypos)
		}
	case window.WindowEventScroll:
		{
			xoff := event.Params[0].(float64)
			yoff := event.Params[1].(float64)
			ScrollHandler(xoff, yoff)
		}
	case window.WindowEventDrop:
		{
			files := event.Params[0].([]string)
			DropHandler(files)
		}
	case window.WindowEventScaleChanged:
		{
			Editor.gridManager.ResetFontSize()
		}
	case window.WindowEventClose:
		{
			if Editor.nvim.connectedViaTcp {
				// Neoray is not responsible for closing neovim.
				Editor.nvim.disconnect()
				// Stop loop
				Editor.quitChan <- true
			} else {
				// Send quit command to neovim and wait until neovim quits.
				Editor.window.KeepAlive()
				go Editor.nvim.execCommand("qa")
			}
		}
	}
}

func ShutdownEditor() {
	Editor.ticker.Stop()
	if Editor.server != nil {
		Editor.server.Close()
	}
	Editor.nvim.Close()
	Editor.contextMenu.Destroy()
	Editor.cursor.Destroy()
	Editor.gridManager.Destroy()
	Editor.window.Destroy()
	glfw.Terminate()
	SetEditorState(EditorDestroyed) // This is actually unnecessary
	logger.Log(logger.DEBUG, "Editor terminated")
}
