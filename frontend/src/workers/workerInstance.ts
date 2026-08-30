// 디코더 워커의 단일 인스턴스를 제공하는 모듈 (streamStore와 타일이 공유)
let instance: Worker | null = null;

// getWorkerInstance는 디코더 워커 싱글턴을 반환한다.
export function getWorkerInstance(): Worker {
  if (!instance) {
    instance = new Worker(new URL('./decoder.worker.ts', import.meta.url), {type: 'module'});
  }
  return instance;
}
