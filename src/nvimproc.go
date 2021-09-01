package main

import (
	// _ "embed"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/neovim/go-client/nvim"
)

const (
	// DEPRECATED
	// All options here are deprecated and will be removed soon
	OPTION_CURSOR_ANIM_DEP  = "neoray_cursor_animation_time"
	OPTION_TRANSPARENCY_DEP = "neoray_background_transparency"
	OPTION_TARGET_TPS_DEP   = "neoray_target_ticks_per_second"
	OPTION_CONTEXT_MENU_DEP = "neoray_context_menu_enabled"
	OPTION_WINDOW_STATE_DEP = "neoray_window_startup_state"
	OPTION_WINDOW_SIZE_DEP  = "neoray_window_startup_size"
	OPTION_KEY_FULLSCRN_DEP = "neoray_key_toggle_fullscreen"
	OPTION_KEY_ZOOMIN_DEP   = "neoray_key_increase_fontsize"
	OPTION_KEY_ZOOMOUT_DEP  = "neoray_key_decrease_fontsize"
)

const (
	// New options
	OPTION_CURSOR_ANIM  = "CursorAnimTime"
	OPTION_TRANSPARENCY = "Transparency"
	OPTION_TARGET_TPS   = "TargetTPS"
	OPTION_CONTEXT_MENU = "ContextMenuOn"
	OPTION_WINDOW_STATE = "WindowState"
	OPTION_WINDOW_SIZE  = "WindowSize"
	OPTION_KEY_FULLSCRN = "KeyFullscreen"
	OPTION_KEY_ZOOMIN   = "KeyZoomIn"
	OPTION_KEY_ZOOMOUT  = "KeyZoomOut"
)

// Add all options here
var OptionsList = []string{
	OPTION_CURSOR_ANIM,
	OPTION_TRANSPARENCY,
	OPTION_TARGET_TPS,
	OPTION_CONTEXT_MENU,
	OPTION_WINDOW_STATE,
	OPTION_WINDOW_SIZE,
	OPTION_KEY_FULLSCRN,
	OPTION_KEY_ZOOMIN,
	OPTION_KEY_ZOOMOUT,
}

type TemporaryOption struct {
	name, value string
}

var OptionSetFuncScript string = `
function NeorayOptionSet(...)
	if a:0 != 2
		echoerr 'NeoraySet needs 2 arguments.'
		return
	endif
	call rpcnotify(CHANID, "NeorayOptionSet", a:1, a:2)
endfunction

function NeorayCompletion(A, L, P)
	return OPTIONLIST
endfunction

command -nargs=+ -complete=customlist,NeorayCompletion NeoraySet call NeorayOptionSet(<f-args>)
`

type NvimProcess struct {
	handle        *nvim.Nvim
	eventReceived AtomicBool
	eventMutex    *sync.Mutex
	eventStack    [][][]interface{}
	optionChanged AtomicBool
	optionMutex   *sync.Mutex
	optionStack   []TemporaryOption
}

func CreateNvimProcess() NvimProcess {
	defer measure_execution_time()()

	proc := NvimProcess{
		eventMutex:  &sync.Mutex{},
		eventStack:  make([][][]interface{}, 0),
		optionMutex: &sync.Mutex{},
		optionStack: make([]TemporaryOption, 0),
	}

	args := append([]string{"--embed"}, editorParsedArgs.others...)

	nv, err := nvim.NewChildProcess(
		nvim.ChildProcessArgs(args...),
		nvim.ChildProcessCommand(editorParsedArgs.execPath))
	if err != nil {
		logMessage(LOG_LEVEL_FATAL, LOG_TYPE_NVIM, "Failed to start neovim instance:", err)
	}
	proc.handle = nv

	logMessage(LOG_LEVEL_DEBUG, LOG_TYPE_NVIM,
		"Neovim started with command:", editorParsedArgs.execPath, mergeStringArray(args))

	return proc
}

// We are initializing some callback functions here because CreateNvimProcess
// copies actual process struct and we lost pointer of it if these functions
// are called in CreateNvimProcess
func (proc *NvimProcess) init() {
	proc.requestApiInfo()
	proc.introduce()
	// Set a variable that users can define their neoray specific customization.
	proc.handle.SetVar("neoray", 1)
	proc.registerScripts()
}

func (proc *NvimProcess) registerScripts() {
	// Replace channel ids in the template
	source := strings.ReplaceAll(OptionSetFuncScript, "CHANID", strconv.Itoa(proc.handle.ChannelID()))
	// Create option list string
	listStr := "["
	for i := 0; i < len(OptionsList); i++ {
		listStr += "'" + OptionsList[i] + "'"
		if i < len(OptionsList)-1 {
			listStr += ","
		}
	}
	listStr += "]"
	// Replace list in source
	source = strings.Replace(source, "OPTIONLIST", listStr, 1)
	// Trim whitespaces
	source = strings.TrimSpace(source)
	// Execute script
	_, err := proc.handle.Exec(source, false)
	if err != nil {
		logMessage(LOG_LEVEL_ERROR, LOG_TYPE_NVIM, "Failed to execute scripts.vim:", err)
		return
	}
	// Register handler
	proc.handle.RegisterHandler("NeorayOptionSet",
		func(iName, iValue interface{}) {
			name, ok1 := iName.(string)
			value, ok2 := iValue.(string)
			if !ok1 || !ok2 {
				// This is not user fault.
				logMessage(LOG_LEVEL_ERROR, LOG_TYPE_NVIM, "NeoraySet arguments are not string.")
				return
			}
			proc.optionMutex.Lock()
			defer proc.optionMutex.Unlock()
			proc.optionStack = append(proc.optionStack, TemporaryOption{
				name: name, value: value,
			})
			proc.optionChanged.Set(true)
		})
}

func (proc *NvimProcess) requestApiInfo() {
	defer measure_execution_time()()

	info, err := proc.handle.APIInfo()
	if err != nil {
		// Maybe fatal?
		logMessage(LOG_LEVEL_ERROR, LOG_TYPE_NVIM, "Failed to get api information:", err)
		return
	}
	// Check the version.
	// info[1] is dictionary of infos and it has a key named 'version',
	// and this key contains a map which has major, minor and patch informations.
	vInfo := reflect.ValueOf(info[1]).MapIndex(reflect.ValueOf("version")).Elem()
	vMajor := vInfo.MapIndex(reflect.ValueOf("major")).Elem().Convert(t_int).Int()
	vMinor := vInfo.MapIndex(reflect.ValueOf("minor")).Elem().Convert(t_int).Int()
	vPatch := vInfo.MapIndex(reflect.ValueOf("patch")).Elem().Convert(t_int).Int()

	if vMinor < 4 {
		logMessage(LOG_LEVEL_FATAL, LOG_TYPE_NVIM,
			"Neoray needs at least 0.4.0 version of neovim. Please update your neovim to a newer version.")
	}

	vStr := fmt.Sprintf("%d.%d.%d", vMajor, vMinor, vPatch)
	logMessage(LOG_LEVEL_TRACE, LOG_TYPE_NVIM, "Neovim version", vStr)
}

func (proc *NvimProcess) introduce() {
	// Short name for the connected client
	name := TITLE
	// Dictionary describing the version
	version := &nvim.ClientVersion{
		Major: VERSION_MAJOR,
		Minor: VERSION_MINOR,
		Patch: VERSION_PATCH,
		// Commit: "",
	}
	if isDebugBuild() {
		version.Prerelease = "dev"
	}
	// Client type
	typ := "ui"
	// Builtin methods in the client
	methods := make(map[string]*nvim.ClientMethod, 0)
	// Arbitrary string:string map of informal client properties
	attributes := make(nvim.ClientAttributes, 1)
	attributes["website"] = WEBPAGE
	attributes["license"] = LICENSE
	err := proc.handle.SetClientInfo(name, version, typ, methods, attributes)
	if err != nil {
		// Maybe fatal?
		logMessage(LOG_LEVEL_ERROR, LOG_TYPE_NVIM, "Failed to set client information:", err)
	}
}

func (proc *NvimProcess) startUI() {
	options := make(map[string]interface{})
	options["rgb"] = true
	options["ext_linegrid"] = true

	if editorParsedArgs.multiGrid {
		options["ext_multigrid"] = true
		logDebug("Multigrid enabled.")
	}

	// TODO: calculate size
	if err := proc.handle.AttachUI(60, 20, options); err != nil {
		logMessage(LOG_LEVEL_FATAL, LOG_TYPE_NVIM, "Attaching ui failed:", err)
	}

	proc.handle.RegisterHandler("redraw",
		func(updates ...[]interface{}) {
			proc.eventMutex.Lock()
			defer proc.eventMutex.Unlock()
			proc.eventStack = append(proc.eventStack, updates)
			proc.eventReceived.Set(true)
		})

	go func() {
		if err := proc.handle.Serve(); err != nil {
			logMessage(LOG_LEVEL_ERROR, LOG_TYPE_NVIM, "Neovim child process closed with errors:", err)
			return
		}
		logMessage(LOG_LEVEL_TRACE, LOG_TYPE_NVIM, "Neovim child process closed.")
		singleton.quitRequested <- true
	}()

	logMessage(LOG_LEVEL_DEBUG, LOG_TYPE_NVIM, "Attached to neovim as an ui client.")
}

func (proc *NvimProcess) update() {
	proc.checkOptions()
}

func (proc *NvimProcess) checkOptions() {
	if proc.optionChanged.Get() {
		proc.optionMutex.Lock()
		defer proc.optionMutex.Unlock()
		for _, opt := range proc.optionStack {
			switch opt.name {
			case OPTION_CURSOR_ANIM:
				value, err := strconv.ParseFloat(opt.value, 32)
				if err != nil {
					logMessage(LOG_LEVEL_WARN, LOG_TYPE_NVIM, OPTION_CURSOR_ANIM, "value isn't valid.")
					break
				}
				if singleton.options.cursorAnimTime != float32(value) {
					logDebugMsg(LOG_TYPE_NVIM, "Option", OPTION_CURSOR_ANIM, "is", opt.value)
					singleton.options.cursorAnimTime = float32(value)
				}
			case OPTION_TRANSPARENCY:
				value, err := strconv.ParseFloat(opt.value, 32)
				if err != nil {
					logMessage(LOG_LEVEL_WARN, LOG_TYPE_NVIM, OPTION_TRANSPARENCY, "value isn't valid.")
					break
				}
				if singleton.options.transparency != float32(value) {
					logDebugMsg(LOG_TYPE_NVIM, "Option", OPTION_TRANSPARENCY, "is", opt.value)
					singleton.options.transparency = float32(value)
					if singleton.mainLoopRunning {
						singleton.fullDraw()
					}
				}
			case OPTION_TARGET_TPS:
				value, err := strconv.Atoi(opt.value)
				if err != nil {
					logMessage(LOG_LEVEL_WARN, LOG_TYPE_NVIM, OPTION_TARGET_TPS, "value isn't valid.")
					break
				}
				if singleton.options.targetTPS != value {
					logDebugMsg(LOG_TYPE_NVIM, "Option", OPTION_TARGET_TPS, "is", value)
					singleton.options.targetTPS = value
					if singleton.mainLoopRunning {
						singleton.resetTicker()
					}
				}
			case OPTION_CONTEXT_MENU:
				value, err := strconv.ParseBool(opt.value)
				if err != nil {
					logMessage(LOG_LEVEL_WARN, LOG_TYPE_NVIM, OPTION_TARGET_TPS, "value isn't valid.")
					break
				}
				if singleton.options.contextMenuEnabled != value {
					logDebugMsg(LOG_TYPE_NVIM, "Option", OPTION_CONTEXT_MENU, "is", value)
					singleton.options.contextMenuEnabled = value
				}
			case OPTION_WINDOW_STATE:
				singleton.window.setState(opt.value)
				logDebugMsg(LOG_TYPE_NVIM, "Option", OPTION_WINDOW_STATE, "is", opt.value)
			case OPTION_WINDOW_SIZE:
				width, height, ok := parseSizeString(opt.value)
				if !ok {
					logMessage(LOG_LEVEL_WARN, LOG_TYPE_NVIM, OPTION_WINDOW_SIZE, "value isn't valid.")
					break
				}
				logDebugMsg(LOG_TYPE_NVIM, "Option", OPTION_WINDOW_SIZE, "is", width, height)
				singleton.window.setSize(width, height, true)
			case OPTION_KEY_FULLSCRN:
				if singleton.options.keyToggleFullscreen != opt.value {
					logDebugMsg(LOG_TYPE_NVIM, "Option", OPTION_KEY_FULLSCRN, "is", opt.value)
					singleton.options.keyToggleFullscreen = opt.value
				}
			case OPTION_KEY_ZOOMIN:
				if singleton.options.keyIncreaseFontSize != opt.value {
					logDebugMsg(LOG_TYPE_NVIM, "Option", OPTION_KEY_ZOOMIN, "is", opt.value)
					singleton.options.keyIncreaseFontSize = opt.value
				}
			case OPTION_KEY_ZOOMOUT:
				if singleton.options.keyDecreaseFontSize != opt.value {
					logDebugMsg(LOG_TYPE_NVIM, "Option", OPTION_KEY_ZOOMOUT, "is", opt.value)
					singleton.options.keyDecreaseFontSize = opt.value
				}
			default:
				logMessage(LOG_LEVEL_WARN, LOG_TYPE_NVIM, "Invalid option", opt.name)
			}
		}
		proc.optionStack = proc.optionStack[0:0]
		proc.optionChanged.Set(false)
	}
}

// DEPRECATED
func (proc *NvimProcess) requestStartupVariables() {
	defer measure_execution_time()()
	options := &singleton.options
	var s string
	var f float32
	var i int
	var b bool
	if proc.handle.Var(OPTION_CURSOR_ANIM_DEP, &f) == nil {
		if f != options.cursorAnimTime {
			logDebugMsg(LOG_TYPE_NVIM, "Deprecated option", OPTION_CURSOR_ANIM_DEP, "is", f)
			options.cursorAnimTime = f
		}
	}
	if proc.handle.Var(OPTION_TRANSPARENCY_DEP, &f) == nil {
		if f != options.transparency {
			logDebugMsg(LOG_TYPE_NVIM, "Deprecated option", OPTION_TRANSPARENCY_DEP, "is", f)
			options.transparency = f
		}
	}
	if proc.handle.Var(OPTION_TARGET_TPS_DEP, &i) == nil {
		if i != options.targetTPS {
			logDebugMsg(LOG_TYPE_NVIM, "Deprecated option", OPTION_TARGET_TPS_DEP, "is", i)
			options.targetTPS = i
		}
	}
	if proc.handle.Var(OPTION_CONTEXT_MENU_DEP, &b) == nil {
		if b != options.contextMenuEnabled {
			logDebugMsg(LOG_TYPE_NVIM, "Deprecated option", OPTION_CONTEXT_MENU_DEP, "is", b)
			options.contextMenuEnabled = b
		}
	}
	if proc.handle.Var(OPTION_KEY_FULLSCRN_DEP, &s) == nil {
		if s != options.keyToggleFullscreen {
			logDebugMsg(LOG_TYPE_NVIM, "Deprecated option", OPTION_KEY_FULLSCRN_DEP, "is", s)
			options.keyToggleFullscreen = s
		}
	}
	if proc.handle.Var(OPTION_KEY_ZOOMIN_DEP, &s) == nil {
		if s != options.keyIncreaseFontSize {
			logDebugMsg(LOG_TYPE_NVIM, "Deprecated option", OPTION_KEY_ZOOMIN_DEP, "is", s)
			options.keyIncreaseFontSize = s
		}
	}
	if proc.handle.Var(OPTION_KEY_ZOOMOUT_DEP, &s) == nil {
		if s != options.keyDecreaseFontSize {
			logDebugMsg(LOG_TYPE_NVIM, "Deprecated option", OPTION_KEY_ZOOMOUT_DEP, "is", s)
			options.keyDecreaseFontSize = s
		}
	}
	// Window startup size
	if proc.handle.Var(OPTION_WINDOW_SIZE_DEP, &s) == nil {
		logDebugMsg(LOG_TYPE_NVIM, "Deprecated option", OPTION_WINDOW_SIZE_DEP, "is", s)
		// Parse the string
		width, height, ok := parseSizeString(s)
		if ok {
			singleton.window.setSize(width, height, true)
		} else {
			logMessage(LOG_LEVEL_WARN, LOG_TYPE_NVIM, "Could not parse size value:", s)
		}
	}
	// Window startup state
	if proc.handle.Var(OPTION_WINDOW_STATE_DEP, &s) == nil {
		logDebugMsg(LOG_TYPE_NVIM, "Deprecated option", OPTION_WINDOW_STATE_DEP, "is", s)
		singleton.window.setState(s)
	}
}

func (proc *NvimProcess) execCommand(format string, args ...interface{}) bool {
	cmd := fmt.Sprintf(format, args...)
	logMessage(LOG_LEVEL_DEBUG, LOG_TYPE_NVIM, "Executing command: [", cmd, "]")
	err := proc.handle.Command(cmd)
	if err != nil {
		logMessage(LOG_LEVEL_ERROR, LOG_TYPE_NVIM, "Command execution failed: [", cmd, "] err:", err)
		return false
	}
	return true
}

func (proc *NvimProcess) currentMode() string {
	mode, err := proc.handle.Mode()
	if err != nil {
		logMessage(LOG_LEVEL_ERROR, LOG_TYPE_NVIM, "Failed to get current mode name:", err)
		return ""
	}
	return mode.Mode
}

func (proc *NvimProcess) echoMsg(format string, args ...interface{}) {
	formatted := fmt.Sprintf(format, args...)
	proc.execCommand("echomsg '%s'", formatted)
}

func (proc *NvimProcess) echoErr(format string, args ...interface{}) {
	formatted := fmt.Sprintf(format, args...)
	proc.handle.WritelnErr(formatted)
}

func (proc *NvimProcess) getRegister(register string) string {
	var content string
	err := proc.handle.Call("getreg", &content, register)
	if err != nil {
		logMessage(LOG_LEVEL_ERROR, LOG_TYPE_NVIM, "Api call getreg() failed:", err)
	}
	return content
}

// This function cuts current selected text and returns the content.
// Not updates clipboard on every system.
func (proc *NvimProcess) cutSelected() string {
	switch proc.currentMode() {
	case "v", "V":
		proc.feedKeys("\"*ygvd")
		return proc.getRegister("*")
	default:
		return ""
	}
}

// This function copies current selected text and returns the content.
// Not updates clipboard on every system.
func (proc *NvimProcess) copySelected() string {
	switch proc.currentMode() {
	case "v", "V":
		proc.feedKeys("\"*y")
		return proc.getRegister("*")
	default:
		return ""
	}
}

// Pastes text at cursor.
func (proc *NvimProcess) paste(str string) {
	err := proc.handle.Call("nvim_paste", nil, str, true, -1)
	if err != nil {
		logMessage(LOG_LEVEL_ERROR, LOG_TYPE_NVIM, "Api call nvim_paste() failed:", err)
	}
}

// TODO: We need to check if this buffer is normal buffer.
// Executing this function in non normal buffers may be dangerous.
func (proc *NvimProcess) selectAll() {
	switch proc.currentMode() {
	case "i", "v":
		proc.feedKeys("<ESC>ggVG")
		break
	case "n":
		proc.feedKeys("ggVG")
		break
	}
}

func (proc *NvimProcess) openFile(file string) {
	proc.execCommand("edit %s", file)
}

func (proc *NvimProcess) gotoLine(line int) {
	logDebug("Goto Line:", line)
	proc.handle.Call("cursor", nil, line, 0)
}

func (proc *NvimProcess) gotoColumn(col int) {
	logDebug("Goto Column:", col)
	proc.handle.Call("cursor", nil, 0, col)
}

func (proc *NvimProcess) feedKeys(keys string) {
	keycode, err := proc.handle.ReplaceTermcodes(keys, true, true, true)
	if err != nil {
		logMessage(LOG_LEVEL_ERROR, LOG_TYPE_NVIM, "Failed to replace termcodes:", err)
		return
	}
	err = proc.handle.FeedKeys(keycode, "m", true)
	if err != nil {
		logMessage(LOG_LEVEL_ERROR, LOG_TYPE_NVIM, "Failed to feed keys:", err)
	}
}

func (proc *NvimProcess) input(keycode string) {
	written, err := proc.handle.Input(keycode)
	if err != nil {
		logMessage(LOG_LEVEL_WARN, LOG_TYPE_NVIM, "Failed to send input keys:", err)
	}
	if written != len(keycode) {
		logMessage(LOG_LEVEL_WARN, LOG_TYPE_NVIM, "Failed to send some keys.")
	}
}

func (proc *NvimProcess) inputMouse(button, action, modifier string, grid, row, column int) {
	err := proc.handle.InputMouse(button, action, modifier, grid, row, column)
	if err != nil {
		logMessage(LOG_LEVEL_WARN, LOG_TYPE_NVIM, "Failed to send mouse input:", err)
	}
}

func (proc *NvimProcess) requestResize(rows, cols int) {
	if rows > 0 && cols > 0 {
		err := proc.handle.TryResizeUI(cols, rows)
		if err != nil {
			logMessage(LOG_LEVEL_ERROR, LOG_TYPE_NVIM, "Failed to send resize request:", err)
			return
		}
	}
}

func (proc *NvimProcess) Close() {
	err := proc.handle.Close()
	if err != nil {
		logMessage(LOG_LEVEL_WARN, LOG_TYPE_NVIM, "Failed to close neovim child process:", err)
	}
}
