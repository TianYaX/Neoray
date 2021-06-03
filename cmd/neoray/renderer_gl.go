package main

import (
	"strings"

	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/veandco/go-sdl2/sdl"
)

const VertexStructSize = 9 * 4

type Vertex struct {
	// These are vertex positions. May not be changed for
	// most of the vertices in runtime.
	X, Y float32 // layout 0
	// These are vertex atlas texture positions for given character
	// and they are changing most of the time.
	TexX, TexY float32 // layout 1
	// These are the background color if useTexture is 0,
	// foreground color if useTexture is 1.
	R, G, B, A float32 // layout 2
	// Use texture is used like a boolean value for determining
	// is this data is for background drawing or foreground drawing.
	useTexture float32 // layout 3
}

var vertexShaderSource = `
#version 330 core

layout(location = 0) in vec2 pos;
layout(location = 1) in vec2 texCoord;
layout(location = 2) in vec4 color;
layout(location = 3) in float useTex;

out vec2 textureCoord;
out vec4 vertexColor;
out float useTexture;

uniform mat4 projection;

void main() {
	gl_Position = vec4(pos, 0, 1) * projection;
	textureCoord = texCoord;
	useTexture = useTex;
	vertexColor = color;
}
` + "\x00"

var fragmentShaderSource = `
#version 330 core

in vec2 textureCoord;
in vec4 vertexColor;
in float useTexture;

uniform sampler2D atlas;

void main() {
	vec4 color = vec4(1);
	if (useTexture > 0.5) {
		color = texture2D(atlas, textureCoord);
		color *= vertexColor;
	} else {
		color = vertexColor;
	}
	gl_FragColor = color;
}
` + "\x00"

// render subsystem global variables
var rgl_context sdl.GLContext
var rgl_vao uint32
var rgl_vbo uint32
var rgl_ebo uint32

var rgl_shader_program uint32
var rgl_atlas_uniform int32
var rgl_projection_uniform int32

var rgl_vertex_buffer_len int
var rgl_element_buffer_len int

func RGL_Init() {
	// Initialize opengl
	context, err := EditorSingleton.window.handle.GLCreateContext()
	if err != nil {
		log_message(LOG_LEVEL_FATAL, LOG_TYPE_RENDERER, "Failed to initialize render context:", err)
	}

	rgl_context = context
	if err = gl.Init(); err != nil {
		log_message(LOG_LEVEL_FATAL, LOG_TYPE_RENDERER, "Failed to initialize opengl:", err)
	}

	// Init shaders
	RGL_InitShaders()
	gl.UseProgram(rgl_shader_program)

	rgl_atlas_uniform = RGL_GetUniformLocation("atlas")
	rgl_projection_uniform = RGL_GetUniformLocation("projection")

	// Initialize vao
	gl.CreateVertexArrays(1, &rgl_vao)
	gl.BindVertexArray(rgl_vao)

	// Initialize vbo
	gl.GenBuffers(1, &rgl_vbo)
	gl.BindBuffer(gl.ARRAY_BUFFER, rgl_vbo)

	// Initialize ebo
	gl.GenBuffers(1, &rgl_ebo)
	gl.BindBuffer(gl.ELEMENT_ARRAY_BUFFER, rgl_ebo)

	// position
	offset := 0
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointerWithOffset(0, 2, gl.FLOAT, false, VertexStructSize, uintptr(offset))
	// texture coords
	offset += 2 * 4
	gl.EnableVertexAttribArray(1)
	gl.VertexAttribPointerWithOffset(1, 2, gl.FLOAT, false, VertexStructSize, uintptr(offset))
	// color
	offset += 2 * 4
	gl.EnableVertexAttribArray(2)
	gl.VertexAttribPointerWithOffset(2, 4, gl.FLOAT, false, VertexStructSize, uintptr(offset))
	// useTexture boolean value
	offset += 4 * 4
	gl.EnableVertexAttribArray(3)
	gl.VertexAttribPointerWithOffset(3, 1, gl.FLOAT, false, VertexStructSize, uintptr(offset))
	// NOTE: If you changed something in Vertex you have to update VertexStructSize!

	gl.Enable(gl.BLEND)
	gl.BlendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA)
	gl.Enable(gl.TEXTURE_2D)

	RGL_CheckError("RGL_Init")
	log_message(LOG_LEVEL_DEBUG, LOG_TYPE_RENDERER, "Opengl Version:", gl.GoStr(gl.GetString(gl.VERSION)))
}

func RGL_GetUniformLocation(name string) int32 {
	uniform_name := gl.Str(name + "\x00")
	loc := gl.GetUniformLocation(rgl_shader_program, uniform_name)
	if loc < 0 {
		log_message(LOG_LEVEL_FATAL, LOG_TYPE_RENDERER, "Failed to find uniform", name)
	}
	return loc
}

func RGL_CreateViewport(w, h int) {
	gl.Viewport(0, 0, int32(w), int32(h))
	// Generate orthographic projection matrix
	var top float32 = 0.0
	var left float32 = 0.0
	var right float32 = float32(w)
	var bottom float32 = float32(h)
	var near float32 = -1.0
	var far float32 = 1.0
	rml, tmb, fmn := (right - left), (top - bottom), (far - near)
	projection_matrix := [16]float32{
		float32(2. / rml), 0, 0, 0, // 1
		0, float32(2. / tmb), 0, 0, // 2
		0, 0, float32(-2. / fmn), 0, // 3
		float32(-(right + left) / rml), // 4
		float32(-(top + bottom) / tmb),
		float32(-(far + near) / fmn), 1}
	// upload projection matrix
	gl.UniformMatrix4fv(rgl_projection_uniform, 1, true, &projection_matrix[0])
}

func RGL_SetAtlasTexture(atlas *Texture) {
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, atlas.id)
	gl.Uniform1i(rgl_atlas_uniform, gl.TEXTURE0)
}

func RGL_ClearScreen(color sdl.Color) {
	gl.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)
	c := u8color_to_fcolor(color)
	gl.ClearColor(c.R, c.G, c.B, c.A)
}

func RGL_UpdateVertexData(data []Vertex) {
	if rgl_vertex_buffer_len != len(data) {
		gl.BufferData(gl.ARRAY_BUFFER, len(data)*VertexStructSize, gl.Ptr(data), gl.STATIC_DRAW)
		RGL_CheckError("RGL_UpdateVertexBufferData.BufferData")
		rgl_vertex_buffer_len = len(data)
	} else {
		gl.BufferSubData(gl.ARRAY_BUFFER, 0, len(data)*VertexStructSize, gl.Ptr(data))
		RGL_CheckError("RGL_UpdateVertexBufferData.BufferSubData")
	}
}

func RGL_UpdateElementData(data []uint32) {
	if rgl_element_buffer_len != len(data) {
		gl.BufferData(gl.ELEMENT_ARRAY_BUFFER, len(data)*4, gl.Ptr(data), gl.STATIC_DRAW)
		RGL_CheckError("RGL_UpdateElementBufferData.BufferData")
		rgl_element_buffer_len = len(data)
	} else {
		gl.BufferSubData(gl.ELEMENT_ARRAY_BUFFER, 0, len(data)*4, gl.Ptr(data))
		RGL_CheckError("RGL_UpdateElementBufferData.BufferSubData")
	}
}

func RGL_Render() {
	// gl.DrawArrays(gl.TRIANGLES, 0, int32(rgl_vertex_buffer_len))
	// RGL_CheckError("RGL_Render.DrawArrays")
	gl.DrawElements(gl.TRIANGLES, int32(rgl_element_buffer_len), gl.UNSIGNED_INT, nil)
	RGL_CheckError("RGL_Render.DrawElements")
}

func RGL_InitShaders() {
	vertexShader := RGL_CompileShader(vertexShaderSource, gl.VERTEX_SHADER)
	fragmentShader := RGL_CompileShader(fragmentShaderSource, gl.FRAGMENT_SHADER)
	rgl_shader_program = gl.CreateProgram()
	gl.AttachShader(rgl_shader_program, vertexShader)
	gl.AttachShader(rgl_shader_program, fragmentShader)
	gl.LinkProgram(rgl_shader_program)
	var status int32
	gl.GetProgramiv(rgl_shader_program, gl.LINK_STATUS, &status)
	if status == gl.FALSE {
		var logLength int32
		gl.GetProgramiv(rgl_shader_program, gl.INFO_LOG_LENGTH, &logLength)
		log := strings.Repeat("\x00", int(logLength+1))
		gl.GetProgramInfoLog(rgl_shader_program, logLength, nil, gl.Str(log))
		log_message(LOG_LEVEL_FATAL, LOG_TYPE_RENDERER,
			"Failed to link shader program:", log)
	}
	gl.DeleteShader(vertexShader)
	gl.DeleteShader(fragmentShader)
}

func RGL_CompileShader(source string, shader_type uint32) uint32 {
	shader := gl.CreateShader(shader_type)
	cstr, free := gl.Strs(source)
	gl.ShaderSource(shader, 1, cstr, nil)
	free()
	gl.CompileShader(shader)
	var result int32
	gl.GetShaderiv(shader, gl.COMPILE_STATUS, &result)
	if result == gl.FALSE {
		var logLength int32
		gl.GetShaderiv(shader, gl.INFO_LOG_LENGTH, &logLength)
		log := strings.Repeat("\x00", int(logLength+1))
		gl.GetShaderInfoLog(shader, logLength, nil, gl.Str(log))
		log_message(LOG_LEVEL_FATAL, LOG_TYPE_RENDERER,
			"Shader compilation failed:", source, log)
	}
	return shader
}

func RGL_CheckError(callerName string) {
	if err := gl.GetError(); err != gl.NO_ERROR {
		var errName string
		switch err {
		case gl.INVALID_ENUM:
			errName = "INVALID_ENUM"
		case gl.INVALID_VALUE:
			errName = "INVALID_VALUE"
		case gl.INVALID_OPERATION:
			errName = "INVALID_OPERATION"
		case gl.STACK_OVERFLOW:
			errName = "STACK_OVERFLOW"
		case gl.STACK_UNDERFLOW:
			errName = "STACK_UNDERFLOW"
		case gl.OUT_OF_MEMORY:
			errName = "OUT_OF_MEMORY"
		case gl.CONTEXT_LOST:
			errName = "CONTEXT_LOST"
		default:
			log_message(LOG_LEVEL_ERROR, LOG_TYPE_RENDERER, "Opengl Error", err, "on", callerName)
			return
		}
		log_message(LOG_LEVEL_ERROR, LOG_TYPE_RENDERER, "Opengl Error", errName, "on", callerName)
	}
}

func RGL_Close() {
	gl.DeleteProgram(rgl_shader_program)
	gl.DeleteBuffers(1, &rgl_vbo)
	gl.DeleteVertexArrays(1, &rgl_vao)
	sdl.GLDeleteContext(rgl_context)
}
