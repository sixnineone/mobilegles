#version 120
attribute vec2 pos;
uniform vec2 uPos;
uniform vec2 uSize;
uniform vec2 uScreenSize;
uniform float uAngle;
varying vec2 vPos;
void main() {
  vec2 p = pos * uSize;
  float c = cos(uAngle);
  float s = sin(uAngle);
  vec2 rotated = vec2(p.x * c - p.y * s, p.x * s + p.y * c);
  vPos = pos;
  vec2 finalPos = uPos + rotated;
  vec2 ndc = finalPos / uScreenSize;
  gl_Position = vec4((ndc.x - 0.5) * 2.0, (0.5 - ndc.y) * 2.0, 0.0, 1.0);
}
