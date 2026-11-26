#version 120
uniform sampler2D uTexture;
uniform vec2 uDir;
uniform vec2 uPixelSize;
varying vec2 vUv;
void main() {
  vec4 col = texture2D(uTexture, vUv) * 0.271;
  vec2 o1 = uDir * uPixelSize;
  vec2 o2 = o1 * 2.0;
  vec2 o3 = o1 * 3.0;
  col += texture2D(uTexture, vUv + o1) * 0.217;
  col += texture2D(uTexture, vUv - o1) * 0.217;
  col += texture2D(uTexture, vUv + o2) * 0.111;
  col += texture2D(uTexture, vUv - o2) * 0.111;
  col += texture2D(uTexture, vUv + o3) * 0.037;
  col += texture2D(uTexture, vUv - o3) * 0.037;
  gl_FragColor = col;
}
