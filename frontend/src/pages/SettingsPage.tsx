// 설정 페이지 — 앱 설정/녹화/보안/백업 통합 관리 (doc 5.6, Phase R.5)
import React, {useCallback, useEffect, useRef, useState} from 'react';
import {AppConfig, BackupFile, SecurityInfo} from '../types/api';
import {api} from '../services/api';
import {recordings, RecordingStatus} from '../services/recordings';
import {useUIStore} from '../store/uiStore';
import {markSelfEdit} from '../store/selfEdits';

const KEY_SOURCE_LABEL: Record<string, string> = {
  env: '환경변수 (WEBNVR_MASTER_KEY)',
  file: '키 파일 (config/.masterkey)',
  fallback: '개발용 폴백 키',
};

function fmtGB(b: number): string {
  if (b >= 1 << 30) return `${(b / (1 << 30)).toFixed(2)} GB`;
  return `${(b / (1 << 20)).toFixed(0)} MB`;
}

// SettingsPage는 앱 설정 편집, 보안 상태, 백업/복원을 제공한다.
export function SettingsPage() {
  const pushToast = useUIStore(s => s.pushToast);
  const [cfg, setCfg] = useState<AppConfig | null>(null);
  const [security, setSecurity] = useState<SecurityInfo | null>(null);
  const [recStatus, setRecStatus] = useState<RecordingStatus | null>(null);
  const [saving, setSaving] = useState(false);
  const fileRef = useRef<HTMLInputElement | null>(null);

  const loadConfig = useCallback((opts?: {silent?: boolean}) => {
    api.health()
      .then(() => api.appConfig())
      .then(setCfg)
      .catch(e => { if (!opts?.silent) pushToast('error', `설정 조회 실패: ${String(e)}`); });
    api.security().then((s: SecurityInfo) => setSecurity(s)).catch(() => setSecurity(null));
    recordings.status().then(setRecStatus).catch(() => setRecStatus(null));
  }, [pushToast]);

  useEffect(() => { loadConfig(); }, [loadConfig]);

  // 다른 클라이언트가 앱 설정을 저장하면 현재 화면을 최신 값으로 갱신한다.
  useEffect(() => {
    const onChange = () => {
      loadConfig({silent: true});
      pushToast('info', '앱 설정이 다른 곳에서 변경되어 새로고침했습니다.');
    };
    window.addEventListener('webnvr-config-changed', onChange);
    return () => window.removeEventListener('webnvr-config-changed', onChange);
  }, [loadConfig, pushToast]);

  const patch = (section: keyof AppConfig, field: string, value: unknown): void => {
    setCfg(c => c ? {
      ...c,
      [section]: {...(c[section] as Record<string, unknown>), [field]: value},
    } as AppConfig : c);
  };

  async function save() {
    if (!cfg) return;
    // 할당량 하향 시 즉시 대량 삭제 경고 (계획서 "할당량 하향 시 즉시 삭제")
    const rec = cfg.recording;
    const usedGB = recStatus ? recStatus.usedBytes / (1 << 30) : 0;
    if (rec.enabled && rec.max_usage_gb > 0 && usedGB > rec.max_usage_gb) {
      const ok = window.confirm(
        `현재 녹화 사용량이 ${fmtGB(recStatus?.usedBytes ?? 0)}인데 한도를 ${rec.max_usage_gb}GB로 저장하려 합니다.\n` +
        '저장하면 한도를 넘는 오래된 녹화가 즉시 삭제됩니다. 계속할까요?');
      if (!ok) return;
    }
    setSaving(true);
    try {
      const saved = await api.updateAppConfig(cfg);
      setCfg(saved);
      pushToast('ok', '설정이 저장되었습니다. 녹화 모드/한도는 즉시 반영되며, 저장 경로·세그먼트 길이는 재시작 후 적용됩니다.');
      recordings.status().then(setRecStatus).catch(() => {});
    } catch (e) {
      pushToast('error', `설정 저장 실패: ${String(e)}`);
    } finally {
      setSaving(false);
    }
  }

  async function exportBackup() {
    try {
      const backup = await api.backup();
      const blob = new Blob([JSON.stringify(backup, null, 2)], {type: 'application/json'});
      const a = document.createElement('a');
      a.href = URL.createObjectURL(blob);
      a.download = `webnvr-backup-${new Date().toISOString().slice(0, 10)}.json`;
      a.click();
      URL.revokeObjectURL(a.href);
      pushToast('ok', '백업 파일을 내려받았습니다. (비밀번호는 포함되지 않습니다)');
    } catch (e) {
      pushToast('error', `백업 실패: ${String(e)}`);
    }
  }

  async function importBackup(file: File) {
    try {
      const backup = JSON.parse(await file.text()) as BackupFile;
      markSelfEdit('restored'); // 되돌아온 cameras_changed 에코로 자기 배지를 띄우지 않게
      const res = await api.restoreBackup(backup);
      pushToast('ok', `${res.restored}대의 카메라가 복원되었습니다. 비밀번호는 다시 입력해야 합니다.`);
    } catch (e) {
      pushToast('error', `복원 실패: ${String(e)}`);
    }
  }

  if (!cfg) {
    return <div className="empty">설정을 불러오는 중…</div>;
  }

  return (
    <>
      <section className="discovery" aria-label="앱 설정">
        <div className="discovery-head">
          <h3>앱 설정</h3>
          <span className="discovery-hint">저장 버튼 클릭 시 즉시 파일에 반영 · ws_port는 재시작 필요</span>
          <button className="btn btn-primary" onClick={save} disabled={saving}>
            {saving ? '저장 중…' : '설정 저장'}
          </button>
        </div>

        <div className="settings-grid">
          <div className="field">
            <label>WS 포트 (재시작 필요)</label>
            <input type="text" value={cfg.server.ws_port}
              onChange={e => patch('server', 'ws_port', Number(e.target.value) || 0)}/>
          </div>
          <div className="field">
            <label>최대 동시 접속 수 (0 = 무제한)</label>
            <input type="text" value={cfg.server.max_clients}
              onChange={e => patch('server', 'max_clients', Number(e.target.value) || 0)}/>
          </div>
          <div className="field">
            <label>기본 전송 방식</label>
            <select value={cfg.stream.default_transport}
              onChange={e => patch('stream', 'default_transport', e.target.value)}>
              <option value="tcp">TCP (안정적)</option>
              <option value="udp">UDP (저지연)</option>
            </select>
          </div>
          <div className="field">
            <label>지터 버퍼 (ms)</label>
            <input type="text" value={cfg.stream.jitter_buffer_ms}
              onChange={e => patch('stream', 'jitter_buffer_ms', Number(e.target.value) || 0)}/>
          </div>
          <div className="field">
            <label>최대 동시 스트림</label>
            <input type="text" value={cfg.stream.max_concurrent_streams}
              onChange={e => patch('stream', 'max_concurrent_streams', Number(e.target.value) || 0)}/>
          </div>
          <div className="field">
            <label>검색 인터페이스 (콤마 구분)</label>
            <input type="text" value={cfg.discovery.scan_interfaces.join(', ')}
              onChange={e => patch('discovery', 'scan_interfaces',
                e.target.value.split(',').map(s => s.trim()).filter(Boolean))}/>
          </div>
          <div className="field">
            <label>하드웨어 디코딩 우선</label>
            <select value={cfg.decoder.prefer_hardware ? 'yes' : 'no'}
              onChange={e => patch('decoder', 'prefer_hardware', e.target.value === 'yes')}>
              <option value="yes">예</option>
              <option value="no">아니오</option>
            </select>
          </div>
        </div>
      </section>

      {cfg.recording && (
        <section className="discovery" aria-label="녹화 설정">
          <div className="discovery-head">
            <h3>녹화</h3>
            <span className="discovery-hint">
              {recStatus
                ? `사용량 ${fmtGB(recStatus.usedBytes)} · 녹화 중 ${recStatus.recording.length}대`
                : '상태 확인 중…'}
              {' · '}카메라별 모드는 카메라 관리에서 설정
            </span>
            <button className="btn btn-primary" onClick={save} disabled={saving}>
              {saving ? '저장 중…' : '설정 저장'}
            </button>
          </div>

          <div className="settings-grid">
            <div className="field">
              <label>녹화 사용</label>
              <select value={cfg.recording.enabled ? 'on' : 'off'}
                onChange={e => patch('recording', 'enabled', e.target.value === 'on')}>
                <option value="off">끔 (기존 동작 유지)</option>
                <option value="on">켬 (record_mode 카메라만 녹화)</option>
              </select>
            </div>
            <div className="field">
              <label>최대 사용량 (GB, 0 = 무제한)</label>
              <input type="text" value={cfg.recording.max_usage_gb}
                onChange={e => patch('recording', 'max_usage_gb', Number(e.target.value) || 0)}/>
            </div>
            <div className="field">
              <label>보관 기간 (일, 0 = 무제한)</label>
              <input type="text" value={cfg.recording.retention_days}
                onChange={e => patch('recording', 'retention_days', Number(e.target.value) || 0)}/>
            </div>
            <div className="field">
              <label>최근 보호 (시간 — 공간 부족해도 유지)</label>
              <input type="text" value={cfg.recording.keep_min_hours}
                onChange={e => patch('recording', 'keep_min_hours', Number(e.target.value) || 0)}/>
            </div>
            <div className="field">
              <label>세그먼트 길이 (초, 재시작 필요)</label>
              <input type="text" value={cfg.recording.segment_seconds}
                onChange={e => patch('recording', 'segment_seconds', Number(e.target.value) || 0)}/>
            </div>
          </div>

          <div className="mono" style={{marginTop: 10, marginBottom: 4, fontSize: 12, color: 'var(--dim)'}}>
            저장 경로 (재시작 필요 — 여유 부족 시 다음 경로로 넘어감)
          </div>
          {cfg.recording.storages.map((s, i) => (
            <div key={i} className="field-row" style={{marginBottom: 6}}>
              <div className="field" style={{flex: 2}}>
                <input type="text" value={s.path} placeholder="예: recordings 또는 /Volumes/USB/recordings"
                  onChange={e => setCfg(c => {
                    if (!c) return c;
                    const storages = [...c.recording.storages];
                    storages[i] = {...storages[i], path: e.target.value};
                    return {...c, recording: {...c.recording, storages}};
                  })}/>
              </div>
              <div className="field">
                <label>여유 하한 %</label>
                <input type="text" value={s.min_free_percent}
                  onChange={e => setCfg(c => {
                    if (!c) return c;
                    const storages = [...c.recording.storages];
                    storages[i] = {...storages[i], min_free_percent: Number(e.target.value) || 0};
                    return {...c, recording: {...c.recording, storages}};
                  })}/>
              </div>
              <div className="field" style={{flex: 0}}>
                <label>&nbsp;</label>
                <button type="button" className="btn"
                  disabled={cfg.recording.storages.length <= 1}
                  onClick={() => setCfg(c => c ? ({
                    ...c,
                    recording: {...c.recording, storages: c.recording.storages.filter((_, k) => k !== i)},
                  }) : c)}>삭제</button>
              </div>
            </div>
          ))}
          <button type="button" className="btn"
            onClick={() => setCfg(c => c ? ({
              ...c,
              recording: {...c.recording, storages: [...c.recording.storages, {path: '', min_free_percent: 5}]},
            }) : c)}>+ 경로 추가</button>

          {recStatus && recStatus.storages.length > 0 && (
            <div className="mono" style={{marginTop: 10, fontSize: 12, color: 'var(--dim)'}}>
              {recStatus.storages.map(s => (
                <div key={s.path}>
                  {s.path} — 여유 {s.freeKnown ? `${s.freePercent.toFixed(1)}%` : '측정 불가'} (하한 {s.minFreePercent}%)
                </div>
              ))}
            </div>
          )}
          <div className="hint" style={{marginTop: 8}}>
            카메라별 녹화(끔/상시/이벤트/둘 다)와 사전·사후 녹화는 <b>카메라 관리</b>의 각 카메라 편집에서 설정합니다.
            이벤트 녹화는 트리거 시점부터 사전 녹화분을 포함한 클립으로 저장되며, 수동 트리거는
            <span className="mono"> POST /api/cameras/&#123;id&#125;/record/event</span> API로 실행합니다.
          </div>
        </section>
      )}

      <section className="discovery" aria-label="보안">
        <div className="discovery-head">
          <h3>보안</h3>
        </div>
        <div className="test-box">
          <div style={{display: 'flex', alignItems: 'center', gap: 8}}>
            마스터 키 출처:
            <span className={`mono ${security?.masterKeySource === 'fallback' ? 'test-fail' : 'test-ok'}`}>
              {security ? KEY_SOURCE_LABEL[security.masterKeySource] ?? security.masterKeySource : '확인 중…'}
            </span>
          </div>
          <div className="mono" style={{marginTop: 6}}>
            {security?.masterKeySource === 'fallback'
              ? '비밀번호 암호화가 고정 개발 키로 되어 있습니다. 환경변수를 설정하거나 앱을 재시작하면 키 파일이 자동 생성·마이그레이션됩니다.'
              : '카메라 비밀번호가 안전하게 암호화되어 있습니다.'}
          </div>
        </div>
      </section>

      <section className="discovery" aria-label="백업 및 복원">
        <div className="discovery-head">
          <h3>백업 및 복원</h3>
        </div>
        <div style={{display: 'flex', gap: 8, alignItems: 'center'}}>
          <button className="btn" onClick={exportBackup}>백업 내보내기 (JSON)</button>
          <button className="btn" onClick={() => fileRef.current?.click()}>백업 가져오기</button>
          <input
            ref={fileRef} type="file" accept="application/json" style={{display: 'none'}}
            onChange={e => {
              const f = e.target.files?.[0];
              if (f) importBackup(f);
              e.target.value = '';
            }}
          />
          <span className="discovery-hint">
            복원 시 현재 카메라 목록이 백업으로 대체되며, 비밀번호는 백업에 포함되지 않아 다시 입력해야 합니다.
          </span>
        </div>
      </section>
    </>
  );
}
