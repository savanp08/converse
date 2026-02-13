<script lang="ts">
    import { goto } from '$app/navigation';
    import AuthModal from '$lib/components/home/AuthModal.svelte';
    import { currentUser, authToken } from '$lib/store';

    let roomName = "";
    let guestUsername = "";

    let isModalOpen = false;
    let isJoining = false;
    let joinError = "";

    async function handleGuestJoin() {
        if (!roomName) return;
        isJoining = true;
        joinError = "";

        const userToJoin = guestUsername || `Guest_${Math.floor(Math.random() * 10000)}`;

        try {
            const res = await fetch('http://localhost:8080/api/rooms/join', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ roomName, username: userToJoin, type: 'ephemeral' })
            });

            const data = await res.json();

            if (!res.ok) throw new Error(data.error || 'Failed to join room');

            currentUser.set({ id: data.userId, username: userToJoin });
            authToken.set(data.token);

            goto(`/chat/${data.roomId}`);

        } catch (e: any) {
            joinError = e.message;
        } finally {
            isJoining = false;
        }
    }

    function onAuthSuccess(event: CustomEvent) {
        const { user, token } = event.detail;

        currentUser.set(user);
        authToken.set(token);

        alert(`Welcome back, ${user.username}!`);
    }
</script>

<div class="container">
    <header>
        <div class="logo">Ephemeral<b>Chat</b></div>
        <button class="btn-login" on:click={() => isModalOpen = true}>
            Log In / Sign Up
        </button>
    </header>

    <main>
        <div class="hero-box">
            <h1>Disappearing chats. <br/>Instant connections.</h1>
            <p>Create a room. Share the link. It vanishes when you leave.</p>

            {#if joinError}
                <div class="error-msg">{joinError}</div>
            {/if}

            <div class="join-form">
                <input 
                    type="text" 
                    placeholder="Enter Room Name (e.g. 'LunchPlan')" 
                    bind:value={roomName} 
                />

                <input 
                    type="text" 
                    placeholder="Username (Optional)" 
                    bind:value={guestUsername} 
                />

                <button on:click={handleGuestJoin} disabled={isJoining || !roomName}>
                    {isJoining ? 'Connecting...' : 'Create / Join Room'}
                </button>
            </div>

            <p class="hint">No signup required for ephemeral rooms.</p>
        </div>
    </main>

    <AuthModal 
        isOpen={isModalOpen} 
        on:close={() => isModalOpen = false} 
        on:success={onAuthSuccess}
    />
</div>

<style>
    :global(body) { margin: 0; font-family: sans-serif; background: #f4f4f4; }

    .container { max-width: 800px; margin: 0 auto; padding: 20px; height: 100vh; display: flex; flex-direction: column; }

    header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 60px; }
    .logo { font-size: 1.5rem; }
    .btn-login { padding: 8px 16px; background: transparent; border: 2px solid #333; cursor: pointer; border-radius: 4px; font-weight: bold; }

    main { flex: 1; display: flex; justify-content: center; align-items: center; }

    .hero-box { text-align: center; background: white; padding: 40px; border-radius: 12px; box-shadow: 0 10px 25px rgba(0,0,0,0.05); width: 100%; max-width: 500px; }

    h1 { margin-top: 0; color: #222; }
    p { color: #666; margin-bottom: 30px; }

    .join-form { display: flex; flex-direction: column; gap: 10px; }
    input { padding: 12px; border: 1px solid #ddd; border-radius: 6px; font-size: 1rem; }
    button { padding: 12px; background: #007bff; color: white; border: none; border-radius: 6px; font-size: 1rem; font-weight: bold; cursor: pointer; transition: background 0.2s; }
    button:disabled { background: #ccc; cursor: not-allowed; }
    button:hover:not(:disabled) { background: #0056b3; }

    .error-msg { color: #d9534f; background: #f9d6d5; padding: 10px; border-radius: 4px; margin-bottom: 15px; }
    .hint { font-size: 0.8rem; color: #999; margin-top: 20px; }
</style>
