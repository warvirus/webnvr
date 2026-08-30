// 카메라 목록 상태와 백엔드 바인딩 호출(낙관적 업데이트 + 롤백)을 관리하는 스토어
import {create} from 'zustand';
import {
  CreateCamera,
  DeleteCamera,
  DiscoverONVIFCameras,
  GetONVIFProfiles,
  GetONVIFStreamURI,
  ListCameras,
  ReorderCameras,
  TestDirectStream,
  TestONVIFCamera,
  UpdateCamera,
} from '../../wailsjs/go/api/CameraService';
import {api, camera} from '../../wailsjs/go/models';

interface CameraState {
  cameras: api.CameraDTO[];
  loaded: boolean;
  discovered: api.DiscoveredCamera[];
  isDiscovering: boolean;
  isBusy: boolean; // 목록 수준 동작(생성/재정렬 등) 진행 표시

  fetchCameras: () => Promise<void>;
  discover: () => Promise<void>;
  addCamera: (req: api.CreateCameraRequest) => Promise<api.CameraDTO | null>;
  updateCamera: (id: string, req: api.UpdateCameraRequest) => Promise<boolean>;
  deleteCamera: (id: string) => Promise<boolean>;
  reorderCameras: (orderedIds: string[]) => Promise<void>;
  testONVIF: (req: api.TestONVIFRequest) => Promise<api.TestONVIFResponse>;
  testDirectStream: (req: api.TestDirectStreamRequest) => Promise<api.TestDirectStreamResponse>;
  getProfiles: (req: api.GetProfilesRequest) => Promise<api.ProfileDTO[]>;
  getStreamURI: (req: api.GetStreamURIRequest) => Promise<string>;
}

export const useCameraStore = create<CameraState>((set, get) => ({
  cameras: [],
  loaded: false,
  discovered: [],
  isDiscovering: false,
  isBusy: false,

  fetchCameras: async () => {
    try {
      const cams = await ListCameras();
      set({cameras: cams, loaded: true});
    } catch (e) {
      set({loaded: true});
      throw e;
    }
  },

  discover: async () => {
    set({isDiscovering: true, discovered: []});
    try {
      const found = await DiscoverONVIFCameras();
      set({discovered: found});
    } finally {
      set({isDiscovering: false});
    }
  },

  addCamera: async (req) => {
    set({isBusy: true});
    try {
      const saved = await CreateCamera(req);
      set({cameras: [...get().cameras, saved]});
      return saved;
    } finally {
      set({isBusy: false});
    }
  },

  updateCamera: async (id, req) => {
    const prev = get().cameras;
    // 낙관적 업데이트: 요청 필드를 즉시 반영하고, 실패하면 롤백한다
    const patch: Record<string, unknown> = {};
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
      cameras: prev.map(c => c.id === id ? new api.CameraDTO({...c, ...patch}) : c),
    });
    try {
      const saved = await UpdateCamera(id, req);
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
      await DeleteCamera(id);
      return true;
    } catch (e) {
      set({cameras: prev}); // 롤백
      throw e;
    }
  },

  reorderCameras: async (orderedIds) => {
    const prev = get().cameras;
    const byId = new Map(prev.map(c => [c.id, c]));
    const reordered = orderedIds
      .map((id, i) => {
        const c = byId.get(id);
        return c ? {...c, layoutOrder: i} : null;
      })
      .filter((c): c is api.CameraDTO => c !== null);
    set({cameras: reordered});
    try {
      await ReorderCameras(orderedIds);
    } catch (e) {
      set({cameras: prev}); // 롤백
      throw e;
    }
  },

  testONVIF: (req) => TestONVIFCamera(req),
  testDirectStream: (req) => TestDirectStream(req),
  getProfiles: (req) => GetONVIFProfiles(req),
  getStreamURI: (req) => GetONVIFStreamURI(req),
}));

// 카메라를 layout_order 순으로 정렬해 반환하는 셀렉터
export function selectOrderedCameras(cameras: api.CameraDTO[]): api.CameraDTO[] {
  return [...cameras].sort((a, b) => a.layoutOrder - b.layoutOrder);
}

// 새 카메라의 기본 스트림 설정
export function defaultStreamConfig(): camera.StreamConfig {
  return new camera.StreamConfig({transport: 'tcp', protocol: 'rtsp', buffer_size: 1024 * 1024});
}
