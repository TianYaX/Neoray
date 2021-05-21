package main

import (
	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/veandco/go-sdl2/sdl"
)

type Texture struct {
	id     uint32
	width  int
	height int
}

func CreateTexture(width, height int) Texture {
	var texture_id uint32
	gl.GenTextures(1, &texture_id)
	gl.BindTexture(gl.TEXTURE_2D, texture_id)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, int32(width), int32(height), 0, gl.RGBA, gl.UNSIGNED_BYTE, nil)
	texture := Texture{
		id:     texture_id,
		width:  width,
		height: height,
	}
	return texture
}

func (texture *Texture) Bind() {
	gl.BindTexture(gl.TEXTURE_2D, texture.id)
}

func (texture *Texture) UpdateFromSurface(surface *sdl.Surface) {
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, surface.W, surface.H, 0, gl.RGBA, gl.UNSIGNED_BYTE, surface.Data())
	RGL_CheckError("UpdateFromSurface")
}

func (texture *Texture) UpdatePartFromSurface(surface *sdl.Surface, dest *sdl.Rect) {
	gl.TexSubImage2D(gl.TEXTURE_2D, 0, dest.X, dest.Y, dest.W, dest.H, gl.RGBA, gl.UNSIGNED_BYTE, surface.Data())
	RGL_CheckError("UpdatePartFromSurface")
}

func (texture *Texture) GetRectGLCoordinates(rect *sdl.Rect) sdl.FRect {
	area := sdl.FRect{}
	area.X = float32(rect.X) / float32(texture.width)
	area.Y = float32(rect.Y) / float32(texture.height)
	area.W = float32(rect.W) / float32(texture.width)
	area.H = float32(rect.H) / float32(texture.height)
	return area
}

func (texture *Texture) Delete() {
	gl.DeleteTextures(1, &texture.id)
}
