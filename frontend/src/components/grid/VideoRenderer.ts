// VideoFrame을 WebGL 텍스처로 직접 업로드해 렌더링하는 클래스 (레터박스 지원)
//
// 설계 노트: doc §9의 YUV 3텍스처 셰이더는 ffmpeg.wasm 폴백(YUV420P 출력)용으로
// 남겨두고, WebCodecs 경로는 VideoFrame을 텍스처로 직접 업로드한다(GPU 변환).
// CPU에서 플레인을 복사하는 것보다 비용이 훨씬 낮다.

const VERT = `#version 300 es
in vec2 a_pos;
out vec2 v_uv;
void main() {
  v_uv = a_pos * 0.5 + 0.5;
  v_uv.y = 1.0 - v_uv.y;
  gl_Position = vec4(a_pos, 0.0, 1.0);
}`;

const FRAG = `#version 300 es
precision mediump float;
in vec2 v_uv;
uniform sampler2D u_tex;
out vec4 fragColor;
void main() {
  fragColor = texture(u_tex, v_uv);
}`;

// VideoRenderer는 캔버스 하나를 소유하며 VideoFrame을 그린다.
// VideoFrame 직접 업로드가 실패하는 WebView에서는 2D 캔버스 우회 경로로 전환한다.
export class VideoRenderer {
  private gl: WebGL2RenderingContext | null = null;
  private program: WebGLProgram | null = null;
  private vao: WebGLVertexArrayObject | null = null;
  private tex: WebGLTexture | null = null;
  private canvas: HTMLCanvasElement;
  private directUploadFailed = false;
  private fallbackCanvas: OffscreenCanvas | HTMLCanvasElement | null = null;
  private fallbackCtx: OffscreenCanvasRenderingContext2D | CanvasRenderingContext2D | null = null;
  private lastErrorLogged = '';

  constructor(canvas: HTMLCanvasElement) {
    this.canvas = canvas;
    const gl = canvas.getContext('webgl2', {
      alpha: false,
      antialias: false,
      preserveDrawingBuffer: false,
    });
    if (!gl) return;
    this.gl = gl;

    const compile = (type: number, src: string): WebGLShader | null => {
      const sh = gl.createShader(type);
      if (!sh) return null;
      gl.shaderSource(sh, src);
      gl.compileShader(sh);
      if (!gl.getShaderParameter(sh, gl.COMPILE_STATUS)) {
        gl.deleteShader(sh);
        return null;
      }
      return sh;
    };

    const vs = compile(gl.VERTEX_SHADER, VERT);
    const fs = compile(gl.FRAGMENT_SHADER, FRAG);
    if (!vs || !fs) return;
    const prog = gl.createProgram();
    if (!prog) return;
    gl.attachShader(prog, vs);
    gl.attachShader(prog, fs);
    gl.linkProgram(prog);
    gl.deleteShader(vs);
    gl.deleteShader(fs);
    if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) {
      gl.deleteProgram(prog);
      return;
    }
    this.program = prog;
    gl.useProgram(prog);

    // 전체 화면 사각형 (-1..1)
    const vao = gl.createVertexArray();
    if (!vao) return;
    this.vao = vao;
    gl.bindVertexArray(vao);
    const buf = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, buf);
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 1, -1, -1, 1, 1, 1]), gl.STATIC_DRAW);
    const loc = gl.getAttribLocation(prog, 'a_pos');
    gl.enableVertexAttribArray(loc);
    gl.vertexAttribPointer(loc, 2, gl.FLOAT, false, 0, 0);

    const tex = gl.createTexture();
    if (!tex) return;
    this.tex = tex;
    gl.bindTexture(gl.TEXTURE_2D, tex);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
    gl.uniform1i(gl.getUniformLocation(prog, 'u_tex'), 0);

    gl.pixelStorei(gl.UNPACK_ALIGNMENT, 1);
    gl.clearColor(0.043, 0.055, 0.078, 1); // --ink
    gl.clear(gl.COLOR_BUFFER_BIT);
  }

  get ready(): boolean {
    return this.gl !== null && this.program !== null && this.tex !== null;
  }

  // draw는 프레임을 종횡비를 유지하며 캔버스에 그린다.
  draw(frame: VideoFrame) {
    const gl = this.gl;
    if (!gl || !this.program || !this.tex) return;

    const cw = this.canvas.clientWidth || 640;
    const ch = this.canvas.clientHeight || 360;
    const dpr = Math.min(window.devicePixelRatio || 1, 2);
    const pw = Math.round(cw * dpr);
    const ph = Math.round(ch * dpr);
    if (this.canvas.width !== pw || this.canvas.height !== ph) {
      this.canvas.width = pw;
      this.canvas.height = ph;
    }

    gl.viewport(0, 0, pw, ph);
    gl.useProgram(this.program);
    gl.bindVertexArray(this.vao);
    gl.activeTexture(gl.TEXTURE0);
    gl.bindTexture(gl.TEXTURE_2D, this.tex);

    // 1차: VideoFrame을 RGBA 텍스처로 직접 업로드 (GPU 변환)
    // 2차: 실패 시 2D 캔버스 우회 (drawImage 후 캔버스를 업로드)
    let source: TexImageSource = frame;
    if (this.directUploadFailed) {
      const fc = this.ensureFallbackCanvas(frame.displayWidth, frame.displayHeight);
      if (!fc || !this.fallbackCtx) {
        this.logOnce('2D 폴백 캔버스를 사용할 수 없습니다');
        return;
      }
      this.fallbackCtx.drawImage(frame, 0, 0);
      source = fc;
    }
    try {
      gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, source);
    } catch (e) {
      if (!this.directUploadFailed) {
        this.directUploadFailed = true;
        this.logOnce(`VideoFrame 직접 업로드 실패, 2D 캔버스 우회로 전환: ${String(e)}`);
        return; // 이 프레임은 건너뛰고 다음 프레임부터 우회 경로 사용
      }
      this.logOnce(`텍스처 업로드 실패: ${String(e)}`);
      return;
    }

    // 레터박스 계산
    const fw = frame.displayWidth || 1;
    const fh = frame.displayHeight || 1;
    const scale = Math.min(pw / fw, ph / fh);
    const vw = Math.round(fw * scale);
    const vh = Math.round(fh * scale);
    gl.viewport((pw - vw) / 2, (ph - vh) / 2, vw, vh);

    gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
  }

  // ensureFallbackCanvas는 2D 우회용 캔버스를 준비한다.
  private ensureFallbackCanvas(w: number, h: number): OffscreenCanvas | HTMLCanvasElement | null {
    if (!this.fallbackCanvas) {
      try {
        this.fallbackCanvas = new OffscreenCanvas(w || 640, h || 360);
        this.fallbackCtx = (this.fallbackCanvas as OffscreenCanvas).getContext('2d');
      } catch {
        this.fallbackCanvas = null;
      }
      if (!this.fallbackCtx && typeof document !== 'undefined') {
        const c = document.createElement('canvas');
        c.width = w || 640;
        c.height = h || 360;
        this.fallbackCanvas = c;
        this.fallbackCtx = c.getContext('2d');
      }
    } else if (this.fallbackCanvas.width !== w || this.fallbackCanvas.height !== h) {
      this.fallbackCanvas.width = w;
      this.fallbackCanvas.height = h;
    }
    return this.fallbackCanvas;
  }

  // logOnce는 같은 오류를 한 번만 기록한다.
  private logOnce(msg: string) {
    if (this.lastErrorLogged !== msg) {
      this.lastErrorLogged = msg;
      console.error('[VideoRenderer]', msg);
    }
  }

  dispose() {
    const gl = this.gl;
    if (!gl) return;
    if (this.tex) gl.deleteTexture(this.tex);
    if (this.vao) gl.deleteVertexArray(this.vao);
    if (this.program) gl.deleteProgram(this.program);
    this.gl = null;
    this.tex = null;
    this.vao = null;
    this.program = null;
  }
}
