import Alpine from 'alpinejs';
import { createPlayground } from './playground.js';

window.Alpine = Alpine;
Alpine.data('playground', createPlayground);
Alpine.start();
