// UI 전역 상태(페이지, 모달, 토스트, 그리드)를 관리하는 Zustand 스토어
import {create} from 'zustand';
import {GridMode} from '../types';

export type Page = 'monitoring' | 'management' | 'settings';

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
  gridPage: number; // 현재 페이지 (0부터 시작)
  zoomReturnMode: GridMode | null; // 더블클릭 확대 시 복귀할 분할 모드 (null이면 확대 아님)
  setPage: (p: Page) => void;
  openCameraModal: (state: CameraModalState) => void;
  closeCameraModal: () => void;
  pushToast: (kind: Toast['kind'], text: string) => void;
  dismissToast: (id: number) => void;
  setGridMode: (m: GridMode) => void;
  setGridPage: (n: number) => void;
  zoomToggle: (cameraIndex: number, orderedLen: number) => void;
}

let toastSeq = 1;

export const useUIStore = create<UIState>((set, get) => ({
  currentPage: 'monitoring',
  cameraModal: null,
  toasts: [],
  gridMode: 'auto',
  gridPage: 0,
  zoomReturnMode: null,
  setPage: (p) => set({currentPage: p}),
  openCameraModal: (state) => set({cameraModal: state}),
  closeCameraModal: () => set({cameraModal: null}),
  pushToast: (kind, text) => {
    const id = toastSeq++;
    set({toasts: [...get().toasts, {id, kind, text}]});
    setTimeout(() => get().dismissToast(id), 4200);
  },
  dismissToast: (id) => set({toasts: get().toasts.filter(t => t.id !== id)}),
  setGridMode: (m) => set({gridMode: m, gridPage: 0, zoomReturnMode: null}), // 분할 버튼 선택 시 페이지 리셋 + 확대 해제
  setGridPage: (n) => set({gridPage: n}),
  // 타일 더블클릭: 확대 아니면 1분할로(해당 카메라 페이지) 전환, 확대 상태면 원래 분할 모드로 복귀
  zoomToggle: (cameraIndex, orderedLen) => {
    const {zoomReturnMode, gridMode, gridPage} = get();
    if (zoomReturnMode === null) {
      set({zoomReturnMode: gridMode, gridMode: 1, gridPage: cameraIndex});
    } else {
      const prev = zoomReturnMode;
      const prevSlots = prev === 'auto' ? Math.max(orderedLen, 1) : prev;
      set({zoomReturnMode: null, gridMode: prev, gridPage: Math.floor(gridPage / prevSlots)});
    }
  },
}));
