#version 120
attribute vec2 aCenter;   // shape center in screen px (top-down)
attribute vec2 aLocal;    // local corner: ±0.5 for rects, 0.5*unit fan for circles
attribute vec2 aHalf;     // half size (px)
attribute float aAngle;   // rotation (radians)
attribute vec4 aColor;
attribute float aShapeType; // 0 = rect, 1 = circle/oval
attribute vec2 aClipMin;  // clip rect, GL window coords (bottom-left origin)
attribute vec2 aClipMax;
uniform vec2 uScreenSize;
varying vec2 vPos;
varying vec4 vColor;
varying float vShapeType;
varying vec2 vClipMin;
varying vec2 vClipMax;
varying vec2 vFragPos; // GL window coords, from the geometry (see shape_fs)
void main() {
  vPos = aLocal;
  vColor = aColor;
  vShapeType = aShapeType;
  vClipMin = aClipMin;
  vClipMax = aClipMax;
  float c = cos(aAngle);
  float s = sin(aAngle);
  vec2 off = aLocal * 2.0 * aHalf;
  vec2 world = aCenter + vec2(c * off.x - s * off.y, s * off.x + c * off.y);
  vFragPos = vec2(world.x, uScreenSize.y - world.y);
  vec2 ndc = world / uScreenSize;
  gl_Position = vec4((ndc.x - 0.5) * 2.0, (0.5 - ndc.y) * 2.0, 0.0, 1.0);
}
