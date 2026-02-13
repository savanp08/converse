<script lang="ts">
    import { createEventDispatcher } from 'svelte';

    export let isOpen: boolean = false;
    let isRegisterMode: boolean = false;

    let email = "";
    let password = "";
    let username = "";
    let error = "";
    let isLoading = false;

    const dispatch = createEventDispatcher();

    function close() {
        dispatch('close');
        error = "";
    }

    async function handleAuth() {
        isLoading = true;
        error = "";

        const endpoint = isRegisterMode ? '/api/auth/register' : '/api/auth/login';

        try {
            const res = await fetch(`http://localhost:8080${endpoint}`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ email, password, username })
            });

            const data = await res.json();

            if (!res.ok) throw new Error(data.error || 'Authentication failed');

            dispatch('success', { user: data.user, token: data.token });
            close();

        } catch (e: any) {
            error = e.message;
        } finally {
            isLoading = false;
        }
    }

    function handleGoogleLogin() {
        window.location.href = "http://localhost:8080/auth/google";
    }
</script>

{#if isOpen}
<div class="modal-backdrop" on:click|self={close}>
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
            {isLoading ? 'Processing...' : (isRegisterMode ? 'Sign Up' : 'Log In')}
        </button>

        <div class="divider">OR</div>

        <button class="btn-google" on:click={handleGoogleLogin}>
            Continue with Google
        </button>

        <p class="switch-mode">
            {isRegisterMode ? 'Already have an account?' : 'Need an account?'}
            <button class="link-btn" on:click={() => isRegisterMode = !isRegisterMode}>
                {isRegisterMode ? 'Log In' : 'Sign Up'}
            </button>
        </p>
    </div>
</div>
{/if}

<style>
    .modal-backdrop {
        position: fixed; top: 0; left: 0; width: 100%; height: 100%;
        background: rgba(0,0,0,0.5); display: flex; justify-content: center; align-items: center;
        z-index: 1000;
    }
    .modal {
        background: white; padding: 2rem; border-radius: 8px; width: 350px;
        box-shadow: 0 4px 6px rgba(0,0,0,0.1);
    }
    .form-group { margin-bottom: 1rem; }
    .form-group label { display: block; margin-bottom: 0.5rem; font-weight: bold; }
    .form-group input { width: 100%; padding: 0.5rem; border: 1px solid #ccc; border-radius: 4px; }
    .btn-primary { width: 100%; padding: 0.75rem; background: #333; color: white; border: none; cursor: pointer; border-radius: 4px;}
    .btn-google { width: 100%; padding: 0.75rem; background: #fff; border: 1px solid #ccc; cursor: pointer; margin-top: 0.5rem; border-radius: 4px;}
    .error-banner { background: #fee; color: #c00; padding: 0.5rem; margin-bottom: 1rem; border-radius: 4px; }
    .divider { text-align: center; margin: 1rem 0; color: #666; font-size: 0.9rem; }
    .switch-mode { text-align: center; margin-top: 1rem; font-size: 0.9rem; }
    .link-btn { background: none; border: none; color: blue; text-decoration: underline; cursor: pointer; }
</style>
