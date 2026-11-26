#version 120
uniform vec4 uColor;
uniform vec4 uColor2;
uniform int uBlendMode;
uniform int uShapeType;
uniform int uGradientType;
varying vec2 vPos;
void main() {
  vec4 src = uColor;
  if (uGradientType > 0) {
    float t = 0.5;
    if (uGradientType == 1) t = clamp(vPos.x + 0.5, 0.0, 1.0);
    else if (uGradientType == 2) t = clamp(vPos.y + 0.5, 0.0, 1.0);
    else if (uGradientType == 3) t = clamp(length(vPos) * 1.414, 0.0, 1.0);
    src = mix(uColor, uColor2, t);
  }
  float edgeDist = 0.5 - max(abs(vPos.x), abs(vPos.y));
  if (uShapeType == 1) { edgeDist = 0.5 - length(vPos); }
  float d = fwidth(edgeDist) * 1.5;
  float coverage = smoothstep(-d, d, edgeDist);
  gl_FragColor = vec4(src.rgb, src.a * coverage);
}
