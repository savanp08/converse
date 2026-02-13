import { writable } from 'svelte/store';
import type { User } from './types';

export const currentUser = writable<User | null>(null);

export const authToken = writable<string | null>(null);
