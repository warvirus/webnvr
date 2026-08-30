// webnvr 프론트엔드 진입점 — 폰트와 전역 스타일 적용
import React from 'react';
import {createRoot} from 'react-dom/client';
import '@fontsource/ibm-plex-sans-kr/400.css';
import '@fontsource/ibm-plex-sans-kr/500.css';
import '@fontsource/ibm-plex-sans-kr/700.css';
import '@fontsource/jetbrains-mono/400.css';
import '@fontsource/jetbrains-mono/600.css';
import './app.css';
import App from './App';

const container = document.getElementById('root')!;

const root = createRoot(container);

root.render(
    <React.StrictMode>
        <App/>
    </React.StrictMode>
);
