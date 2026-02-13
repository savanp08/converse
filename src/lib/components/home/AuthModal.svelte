<script lang="ts">
	import { createEventDispatcher } from 'svelte';
	const API_BASE = (import.meta.env.VITE_API_BASE as string | undefined) ?? 'http://localhost:8080';
	const CLIENT_LOG_PREFIX = '[auth-client]';

	export let isOpen: boolean = false;
	let isRegisterMode: boolean = false;

	let email = '';
	let password = '';
	let username = '';
	let error = '';
	let isLoading = false;

	const dispatch = createEventDispatcher();

	function clientLog(event: string, payload?: unknown) {
		const timestamp = new Date().toISOString();
		if (payload === undefined) {
			console.log(`${CLIENT_LOG_PREFIX} ${timestamp} ${event}`);
			return;
		}
		console.log(`${CLIENT_LOG_PREFIX} ${timestamp} ${event}`, payload);
	}

	function close() {
		dispatch('close');
		error = '';
	}

	async function handleAuth() {
		isLoading = true;
		error = '';

		const endpoint = isRegisterMode ? '/api/auth/signup' : '/api/auth/login';

		try {
			clientLog('api-auth-request', {
				mode: isRegisterMode ? 'signup' : 'login',
				endpoint,
				email,
				username
			});
			const res = await fetch(`${API_BASE}${endpoint}`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ email, password, username })
			});

			const data = await res.json().catch(() => ({}));
			clientLog('api-auth-response', { endpoint, status: res.status, ok: res.ok, data });

			if (!res.ok) throw new Error(data.error || data.message || 'Authentication failed');

			dispatch('success', { user: data.user, token: data.token });
			close();
		} catch (e: any) {
			clientLog('api-auth-error', { endpoint, error: e?.message ?? String(e) });
			error = e.message;
		} finally {
			isLoading = false;
		}
	}

	function handleGoogleLogin() {
		clientLog('google-login-clicked');
		error = 'Google login is not configured yet';
	}
</script>

{#if isOpen}
	<div
		class="modal-backdrop"
		role="button"
		tabindex="0"
		aria-label="Close auth modal"
		on:click|self={close}
		on:keydown={(event) => {
			if (event.key === 'Escape' || event.key === 'Enter' || event.key === ' ') {
				event.preventDefault();
				close();
			}
		}}
	>
		<div class="modal">
			<h2>{isRegisterMode ? 'Create Account' : 'Welcome Back'}</h2>

			{#if error}
				<div class="error-banner">{error}</div>
			{/if}

			<div class="form-group">
				<label for="email">Email</label>
				<input type="email" id="email" bind:value={email} placeholder="you@example.com" />
			</div>

			{#if isRegisterMode}
				<div class="form-group">
					<label for="username">Username</label>
					<input type="text" id="username" bind:value={username} placeholder="CoolUser123" />
				</div>
			{/if}

			<div class="form-group">
				<label for="password">Password</label>
				<input type="password" id="password" bind:value={password} placeholder="••••••••" />
			</div>

			<button class="btn-primary" on:click={handleAuth} disabled={isLoading}>
				{isLoading ? 'Processing...' : isRegisterMode ? 'Sign Up' : 'Log In'}
			</button>

			<div class="divider">OR</div>

			<button class="btn-google" on:click={handleGoogleLogin}> Continue with Google </button>

			<p class="switch-mode">
				{isRegisterMode ? 'Already have an account?' : 'Need an account?'}
				<button class="link-btn" on:click={() => (isRegisterMode = !isRegisterMode)}>
					{isRegisterMode ? 'Log In' : 'Sign Up'}
				</button>
			</p>
		</div>
	</div>
{/if}

<style>
	.modal-backdrop {
		position: fixed;
		top: 0;
		left: 0;
		width: 100%;
		height: 100%;
		background: rgba(0, 0, 0, 0.5);
		display: flex;
		justify-content: center;
		align-items: center;
		z-index: 1000;
	}
	.modal {
		background: white;
		padding: 2rem;
		border-radius: 8px;
		width: 350px;
		box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);
	}
	.form-group {
		margin-bottom: 1rem;
	}
	.form-group label {
		display: block;
		margin-bottom: 0.5rem;
		font-weight: bold;
	}
	.form-group input {
		width: 100%;
		padding: 0.5rem;
		border: 1px solid #ccc;
		border-radius: 4px;
	}
	.btn-primary {
		width: 100%;
		padding: 0.75rem;
		background: #333;
		color: white;
		border: none;
		cursor: pointer;
		border-radius: 4px;
	}
	.btn-google {
		width: 100%;
		padding: 0.75rem;
		background: #fff;
		border: 1px solid #ccc;
		cursor: pointer;
		margin-top: 0.5rem;
		border-radius: 4px;
	}
	.error-banner {
		background: #fee;
		color: #c00;
		padding: 0.5rem;
		margin-bottom: 1rem;
		border-radius: 4px;
	}
	.divider {
		text-align: center;
		margin: 1rem 0;
		color: #666;
		font-size: 0.9rem;
	}
	.switch-mode {
		text-align: center;
		margin-top: 1rem;
		font-size: 0.9rem;
	}
	.link-btn {
		background: none;
		border: none;
		color: blue;
		text-decoration: underline;
		cursor: pointer;
	}
</style>
