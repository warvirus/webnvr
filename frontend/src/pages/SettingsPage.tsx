// 설정 페이지 — 앱 설정/보안/백업 통합 관리 (doc 5.6)
import React, {useEffect, useRef, useState} from 'react';
import {AppConfig, BackupFile, SecurityInfo} from '../types/api';
import {api} from '../services/api';
import {useUIStore} from '../store/uiStore';

const KEY_SOURCE_LABEL: Record<string, string> = {
  env: '환경변수 (WEBNVR_MASTER_KEY)',
  file: '키 파일 (config/.masterkey)',
  fallback: '개발용 폴백 키',
};

// SettingsPage는 앱 설정 편집, 보안 상태, 백업/복원을 제공한다.
export function SettingsPage() {
  const pushToast = useUIStore(s => s.pushToast);
  const [cfg, setCfg] = useState<AppConfig | null>(null);
  const [security, setSecurity] = useState<SecurityInfo | null>(null);
  const [saving, setSaving] = useState(false);
  const fileRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    api.health()
      .then(() => api.appConfig())
      .then(setCfg)
      .catch(e => pushToast('error', `설정 조회 실패: ${String(e)}`));
    api.security().then((s: SecurityInfo) => setSecurity(s)).catch(() => setSecurity(null));
  }, [pushToast]);

  const patch = (section: keyof AppConfig, field: string, value: unknown): void => {
    setCfg(c => c ? {
      ...c,
      [section]: {...(c[section] as Record<string, unknown>), [field]: value},
    } as AppConfig : c);
  };

  async function save() {
    if (!cfg) return;
    setSaving(true);
    try {
      const saved = await api.updateAppConfig(cfg);
      setCfg(saved);
      pushToast('ok', '설정이 저장되었습니다. 일부 항목(ws_port 등)은 앱 재시작 후 적용됩니다.');
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
