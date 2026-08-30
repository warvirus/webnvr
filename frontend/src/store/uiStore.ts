// UI 전역 상태(페이지, 모달, 토스트, 그리드)를 관리하는 Zustand 스토어
import {create} from 'zustand';
import {GridMode} from '../types';

export type Page = 'monitoring' | 'management';

export interface Toast {
  id: number;
  kind: 'ok' | 'error' | 'info';
  text: string;
}

export interface CameraModalState {
  mode: 'add' | 'edit';
  cameraId?: string; // edit일 때 대상 카메라
  presetXAddr?: string; // 검색 결과에서 열 때 미리 채울 주소
}

interface UIState {
  currentPage: Page;
  cameraModal: CameraModalState | null;
  toasts: Toast[];
  gridMode: GridMode;
  setPage: (p: Page) => void;
  openCameraModal: (state: CameraModalState) => void;
  closeCameraModal: () => void;
  pushToast: (kind: Toast['kind'], text: string) => void;
  dismissToast: (id: number) => void;
  setGridMode: (m: GridMode) => void;
}

let toastSeq = 1;

export const useUIStore = create<UIState>((set, get) => ({
  currentPage: 'management',
  cameraModal: null,
  toasts: [],
  gridMode: 'auto',
  setPage: (p) => set({currentPage: p}),
  openCameraModal: (state) => set({cameraModal: state}),
  closeCameraModal: () => set({cameraModal: null}),
  pushToast: (kind, text) => {
    const id = toastSeq++;
    set({toasts: [...get().toasts, {id, kind, text}]});
    setTimeout(() => get().dismissToast(id), 4200);
  },
  dismissToast: (id) => set({toasts: get().toasts.filter(t => t.id !== id)}),
  setGridMode: (m) => set({gridMode: m}),
}));
