import './style.css';
import { mount } from 'svelte';
import App from './App.svelte';

// Svelte 5 mount API (the class component constructor was removed).
const app = mount(App, {
  target: document.getElementById('app'),
});

export default app;
