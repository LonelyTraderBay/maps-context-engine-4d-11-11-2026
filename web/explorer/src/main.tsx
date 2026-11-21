import { createRoot } from 'react-dom/client';
import '@xyflow/react/dist/style.css';
import './explorer.css';
import { App } from './shell/App.tsx';

const root = document.getElementById('root');
if (!root) throw new Error('Explorer root is unavailable');
createRoot(root).render(<App />);
