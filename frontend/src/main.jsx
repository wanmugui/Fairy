import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import VoicePage from './pages/VoicePage'
import './styles/App.css'
import './styles/voice.css'

function getRoute() {
  if (window.location.hash.startsWith('#/voice')) return 'voice';
  if (window.location.pathname.replace(/\/+$/, '') === '/voice') return 'voice';
  return 'app';
}

function Root() {
  const [route, setRoute] = React.useState(getRoute());
  React.useEffect(() => {
    const sync = () => setRoute(getRoute());
    window.addEventListener('hashchange', sync);
    return () => window.removeEventListener('hashchange', sync);
  }, []);
  return route === 'voice' ? <VoicePage /> : <App />;
}

ReactDOM.createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <Root />
  </React.StrictMode>,
)
