<script lang="ts">
	import { page } from '$app/stores';
	import { onMount } from 'svelte';

	let showToast = false;
	let toastMessage = '';

	$: roomId = $page.params.roomId ?? '';

	onMount(() => {
		toastMessage = `Joined Room: ${roomId}`;
		showToast = true;

		const toastTimer = setTimeout(() => {
			showToast = false;
		}, 3000);

		return () => clearTimeout(toastTimer);
	});
</script>

{#if showToast}
	<div class="toast" role="status" aria-live="polite">
		{toastMessage}
	</div>
{/if}

<main class="chat-page">
	<section class="chat-card">
		<h1>Room: {roomId}</h1>
		<p>Connected and ready.</p>
	</section>
</main>

<style>
	.chat-page {
		min-height: 100vh;
		display: grid;
		place-items: center;
		padding: 2rem 1rem;
		background: linear-gradient(160deg, #f8fafc 0%, #eef2ff 100%);
	}

	.chat-card {
		width: min(640px, 100%);
		background: #ffffff;
		border-radius: 14px;
		padding: 2rem;
		box-shadow: 0 14px 40px rgba(15, 23, 42, 0.08);
	}

	h1 {
		margin: 0 0 0.5rem;
		color: #0f172a;
		font-size: clamp(1.4rem, 2vw, 1.9rem);
	}

	p {
		margin: 0;
		color: #475569;
	}

	.toast {
		position: fixed;
		top: 1rem;
		left: 50%;
		transform: translateX(-50%);
		background: #1f2937;
		color: #ffffff;
		padding: 0.7rem 1.1rem;
		border-radius: 999px;
		font-weight: 600;
		letter-spacing: 0.01em;
		box-shadow: 0 10px 24px rgba(0, 0, 0, 0.25);
		z-index: 9999;
		pointer-events: none;
	}
</style>
