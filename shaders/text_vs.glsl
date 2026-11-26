#version 120
attribute vec2 pos;
attribute vec2 uv;
attribute vec4 color;
attribute vec2 clipMin;
attribute vec2 clipMax;
uniform vec2 uScreenSize;
varying vec2 vUv;
varying vec4 vColor;
varying vec2 vClipMin;
varying vec2 vClipMax;
varying vec2 vFragPos; // GL window coords, from the geometry (see text_fs)
void main() {
  vUv = uv;
  vColor = color;
  vClipMin = clipMin;
  vClipMax = clipMax;
  vFragPos = vec2(pos.x, uScreenSize.y - pos.y);
  vec2 ndc = pos / uScreenSize;
  gl_Position = vec4((ndc.x - 0.5) * 2.0, (0.5 - ndc.y) * 2.0, 0.0, 1.0);
}
