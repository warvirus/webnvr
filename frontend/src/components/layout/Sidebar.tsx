// 좌측 네비게이션 레일
import {IconCamera, IconGrid} from '../common/Icons';
import {Page, useUIStore} from '../../store/uiStore';

const NAV: {page: Page; label: string; icon: React.ReactNode}[] = [
  {page: 'monitoring', label: '모니터링', icon: <IconGrid size={18}/>},
  {page: 'management', label: '카메라 관리', icon: <IconCamera size={18}/>},
];

export function Sidebar() {
  const {currentPage, setPage} = useUIStore();
  return (
    <nav className="rail" aria-label="주 네비게이션">
      <div className="rail-brand">WEBNVR</div>
      {NAV.map(item => (
        <button
          key={item.page}
          className={`rail-btn ${currentPage === item.page ? 'active' : ''}`}
          onClick={() => setPage(item.page)}
          title={item.label}
          aria-label={item.label}
          aria-current={currentPage === item.page ? 'page' : undefined}
        >
          {item.icon}
        </button>
      ))}
      <div className="rail-spacer"/>
    </nav>
  );
}
