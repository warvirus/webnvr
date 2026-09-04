// 카메라 목록 상태와 백엔드 HTTP API 호출(낙관적 업데이트 + 롤백)을 관리하는 스토어
import {create} from 'zustand';
import {api} from '../services/api';
import {markSelfEdit} from './selfEdits';
import {
  CameraDTO,
  CreateCameraRequest,
  DiscoveredCamera,
  GetProfilesRequest,
  PresetDTO,
  ProfileDTO,
  StreamConfig,
  TestDirectStreamRequest,
  TestDirectStreamResponse,
  TestONVIFRequest,
  TestONVIFResponse,
  UpdateCameraRequest,
} from '../types/api';

interface CameraState {
  cameras: CameraDTO[];
  loaded: boolean;
  discovered: DiscoveredCamera[];
  isDiscovering: boolean;
  isBusy: boolean; // 목록 수준 동작(생성/재정렬 등) 진행 표시

  fetchCameras: () => Promise<void>;
  discover: () => Promise<void>;
  addCamera: (req: CreateCameraRequest) => Promise<CameraDTO | null>;
  updateCamera: (id: string, req: UpdateCameraRequest) => Promise<boolean>;
  deleteCamera: (id: string) => Promise<boolean>;
  reorderCameras: (orderedIds: string[]) => Promise<void>;
  testONVIF: (req: TestONVIFRequest) => Promise<TestONVIFResponse>;
  testDirectStream: (req: TestDirectStreamRequest) => Promise<TestDirectStreamResponse>;
  getProfiles: (req: GetProfilesRequest) => Promise<ProfileDTO[]>;
  getCameraProfiles: (cameraId: string) => Promise<ProfileDTO[]>;
  getCameraStreamURI: (cameraId: string) => Promise<string>;
  getCameraPresets: (cameraId: string) => Promise<PresetDTO[]>;
}

export const useCameraStore = create<CameraState>((set, get) => ({
  cameras: [],
  loaded: false,
  discovered: [],
  isDiscovering: false,
  isBusy: false,

  fetchCameras: async () => {
    const cams = await api.listCameras();
    set({cameras: cams, loaded: true});
  },

  discover: async () => {
    set({isDiscovering: true, discovered: []});
    try {
      const found = await api.discover();
      set({discovered: found});
    } finally {
      set({isDiscovering: false});
    }
  },

  addCamera: async (req) => {
    set({isBusy: true});
    try {
      const saved = await api.createCamera(req);
      set({cameras: [...get().cameras, saved]});
      return saved;
    } finally {
      set({isBusy: false});
    }
  },

  updateCamera: async (id, req) => {
    markSelfEdit('updated', id); // 되돌아온 cameras_changed 에코로 자기 배지를 띄우지 않게
    const prev = get().cameras;
    // 낙관적 업데이트: 요청 필드를 즉시 반영하고, 실패하면 롤백한다
    const patch: Partial<CameraDTO> = {};
    if (req.name != null) patch.name = req.name;
    if (req.xaddr != null) patch.xaddr = req.xaddr;
    if (req.username != null) patch.username = req.username;
    if (req.profileToken != null) patch.profileToken = req.profileToken;
    if (req.streamUrl != null) patch.streamUrl = req.streamUrl;
    if (req.streamConfig != null) patch.streamConfig = req.streamConfig;
    if (req.ptzSupported != null) patch.ptzSupported = req.ptzSupported;
    if (req.groupId != null) patch.groupId = req.groupId;
    if (req.enabled != null) patch.enabled = req.enabled;
    if (req.password != null) patch.hasPassword = true;

    set({
      cameras: prev.map(c => c.id === id ? {...c, ...patch} : c),
    });
    try {
      const saved = await api.updateCamera(id, req);
      set({cameras: get().cameras.map(c => c.id === id ? saved : c)});
      return true;
    } catch (e) {
      set({cameras: prev}); // 롤백
      throw e;
    }
  },

  deleteCamera: async (id) => {
    const prev = get().cameras;
    set({cameras: prev.filter(c => c.id !== id)});
    try {
      await api.deleteCamera(id);
      return true;
    } catch (e) {
      set({cameras: prev}); // 롤백
      throw e;
    }
  },

  reorderCameras: async (orderedIds) => {
    markSelfEdit('reordered');
    const prev = get().cameras;
    const byId = new Map(prev.map(c => [c.id, c]));
    const reordered = orderedIds
      .map((id, i) => {
        const c = byId.get(id);
        return c ? {...c, layoutOrder: i} : null;
      })
      .filter((c): c is CameraDTO => c !== null);
    set({cameras: reordered});
    try {
      await api.reorderCameras(orderedIds);
    } catch (e) {
      set({cameras: prev}); // 롤백
      throw e;
    }
  },

  testONVIF: (req) => api.testONVIF(req),
  testDirectStream: (req) => api.testDirectStream(req),
  getProfiles: (req) => api.onvifProfiles(req),
  getCameraProfiles: (cameraId) => api.cameraProfiles(cameraId),
  getCameraStreamURI: (cameraId) => api.cameraStreamURI(cameraId).then(r => r.uri),
  getCameraPresets: (cameraId) => api.cameraPresets(cameraId),
}));

// 카메라를 layout_order 순으로 정렬해 반환하는 셀렉터
export function selectOrderedCameras(cameras: CameraDTO[]): CameraDTO[] {
  return [...cameras].sort((a, b) => a.layoutOrder - b.layoutOrder);
}

// 새 카메라의 기본 스트림 설정
export function defaultStreamConfig(): StreamConfig {
  return {transport: 'tcp', protocol: 'rtsp', buffer_size: 1024 * 1024};
}
