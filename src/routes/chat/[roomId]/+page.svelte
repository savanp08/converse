<script lang="ts">
	import { browser } from '$app/environment';
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import { currentUser } from '$lib/store';
	import { onDestroy, tick } from 'svelte';

	type ConnectionState = 'idle' | 'connecting' | 'open' | 'closed' | 'error';

	type ChatMessage = {
		id: string;
		roomId: string;
		senderId: string;
		senderName: string;
		content: string;
		type: string;
		createdAt: number;
		pending?: boolean;
	};

	type ChatThread = {
		id: string;
		name: string;
		lastMessage: string;
		lastActivity: number;
		unread: number;
	};

	type OnlineMember = {
		id: string;
		name: string;
		isOnline: boolean;
	};

	const CLIENT_LOG_PREFIX = '[chat-client]';

	const API_BASE = (import.meta.env.VITE_API_BASE as string | undefined) ?? 'http://localhost:8080';
	const WS_BASE = (import.meta.env.VITE_WS_BASE as string | undefined) ?? 'ws://localhost:8080';

	let ws: WebSocket | null = null;
	let wsRoomId = '';
	let wsState: ConnectionState = 'idle';
	let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
	let reconnectAttempts = 0;

	let toastMessage = '';
	let showToast = false;
	let toastTimer: ReturnType<typeof setTimeout> | null = null;
	let lastToastRoom = '';

	let chatListSearch = '';
	let roomMessageSearch = '';
	let draftMessage = '';
	let attachedFile: File | null = null;
	let showLeftMenu = false;
	let showRoomMenu = false;
	let showRoomSearch = false;
	let showRoomInfo = false;

	let roomThreads: ChatThread[] = [];
	let messagesByRoom: Record<string, ChatMessage[]> = {};
	let onlineByRoom: Record<string, OnlineMember[]> = {};
	let pendingOutgoingByRoom: Record<string, ChatMessage[]> = {};
	let isExtendingRoom = false;

	let fileInput: HTMLInputElement | null = null;
	let messageViewport: HTMLDivElement | null = null;
	let lastRenderedMessageCount = 0;

	$: roomId = decodeURIComponent($page.params.roomId ?? '');
	$: roomNameFromURL = decodeURIComponent($page.url.searchParams.get('name') ?? '').trim();
	$: currentUserId = $currentUser?.id ?? 'guest';
	$: currentUsername = $currentUser?.username ?? 'Guest';
	$: activeThread =
		roomThreads.find((thread) => thread.id === roomId) ?? createThread(roomId || 'default-room');
	$: currentMessages = messagesByRoom[roomId] ?? [];
	$: currentOnlineMembers = onlineByRoom[roomId] ?? [];
	$: activeUnreadCount = activeThread?.unread ?? 0;
	$: connectionLabel = getConnectionLabel(wsState);
	$: filteredThreads = getFilteredThreads(
		roomThreads,
		chatListSearch,
		messagesByRoom,
		roomId,
		activeThread.name
	);
	$: visibleMessages = getVisibleMessages(currentMessages, roomMessageSearch);
	$: if (roomId) {
		ensureRoomThread(roomId, roomNameFromURL || undefined);
		ensureOnlineSeed(roomId);
	}
	$: if (browser && roomId && roomId !== wsRoomId) {
		connectToRoom(roomId);
	}
	$: if (browser && roomId && roomId !== lastToastRoom) {
		showJoinToast(roomId);
	}
	$: if (visibleMessages.length !== lastRenderedMessageCount) {
		lastRenderedMessageCount = visibleMessages.length;
		void tick().then(scrollMessagesToBottom);
	}

	onDestroy(() => {
		clientLog('component-destroy', { roomId, wsRoomId, wsState });
		closeSocket();
		clearReconnectTimer();
		clearToastTimer();
	});

	function clientLog(event: string, payload?: unknown) {
		const timestamp = new Date().toISOString();
		if (payload === undefined) {
			console.log(`${CLIENT_LOG_PREFIX} ${timestamp} ${event}`);
			return;
		}
		console.log(`${CLIENT_LOG_PREFIX} ${timestamp} ${event}`, payload);
	}

	function clearReconnectTimer() {
		if (reconnectTimer) {
			clearTimeout(reconnectTimer);
			reconnectTimer = null;
		}
	}

	function clearToastTimer() {
		if (toastTimer) {
			clearTimeout(toastTimer);
			toastTimer = null;
		}
	}

	function showJoinToast(activeRoomId: string) {
		clientLog('toast-joined-room', { roomId: activeRoomId });
		lastToastRoom = activeRoomId;
		toastMessage = `Joined Room: ${activeRoomId}`;
		showToast = true;
		clearToastTimer();
		toastTimer = setTimeout(() => {
			showToast = false;
		}, 3000);
	}

	function createThread(id: string, nameOverride?: string): ChatThread {
		const defaultName = nameOverride ?? formatRoomName(id);
		return {
			id,
			name: defaultName,
			lastMessage: '',
			lastActivity: Date.now(),
			unread: 0
		};
	}

	function ensureRoomThread(targetRoomId: string, nameOverride?: string) {
		const existing = roomThreads.find((thread) => thread.id === targetRoomId);
		if (existing) {
			if (nameOverride && existing.name !== nameOverride) {
				roomThreads = sortThreads(
					roomThreads.map((thread) =>
						thread.id === targetRoomId ? { ...thread, name: nameOverride } : thread
					)
				);
			}
			return;
		}

		const nextThread = createThread(targetRoomId, nameOverride);
		roomThreads = sortThreads([nextThread, ...roomThreads]);
	}

	function updateThreadPreview(targetRoomId: string) {
		const roomMessages = messagesByRoom[targetRoomId] ?? [];
		const lastMessage = roomMessages[roomMessages.length - 1];
		const fallbackName = formatRoomName(targetRoomId);

		if (!lastMessage) {
			ensureRoomThread(targetRoomId, fallbackName);
			return;
		}

		const merged = roomThreads.some((thread) => thread.id === targetRoomId)
			? roomThreads.map((thread) =>
					thread.id === targetRoomId
						? {
								...thread,
								name: thread.name || fallbackName,
								lastMessage: lastMessage.content,
								lastActivity: lastMessage.createdAt
							}
						: thread
				)
			: [
					{
						id: targetRoomId,
						name: fallbackName,
						lastMessage: lastMessage.content,
						lastActivity: lastMessage.createdAt,
						unread: 0
					},
					...roomThreads
				];

		roomThreads = sortThreads(merged);
	}

	function sortThreads(threads: ChatThread[]) {
		return [...threads].sort((a, b) => b.lastActivity - a.lastActivity);
	}

	function ensureOnlineSeed(targetRoomId: string) {
		if (onlineByRoom[targetRoomId]?.length) {
			return;
		}

		const me = { id: currentUserId, name: currentUsername, isOnline: true };
		onlineByRoom = {
			...onlineByRoom,
			[targetRoomId]: dedupeMembers([me])
		};
	}

	function selectRoom(targetRoomId: string) {
		if (!targetRoomId || targetRoomId === roomId) {
			return;
		}
		clientLog('select-room', { fromRoom: roomId, toRoom: targetRoomId });
		showLeftMenu = false;
		showRoomMenu = false;
		showRoomSearch = false;
		roomMessageSearch = '';
		showRoomInfo = false;
		const selected = roomThreads.find((thread) => thread.id === targetRoomId);
		const roomNameQuery = selected?.name ? `?name=${encodeURIComponent(selected.name)}` : '';
		void goto(`/chat/${encodeURIComponent(targetRoomId)}${roomNameQuery}`);
	}

	function connectToRoom(targetRoomId: string) {
		if (!browser || !targetRoomId) {
			return;
		}

		clientLog('ws-connect-start', { targetRoomId, wsBase: WS_BASE });

		clearReconnectTimer();
		closeSocket();
		wsState = 'connecting';
		wsRoomId = targetRoomId;

		try {
			const wsURL = new URL(`${WS_BASE}/ws/${encodeURIComponent(targetRoomId)}`);
			wsURL.searchParams.set('userId', currentUserId);
			wsURL.searchParams.set('username', currentUsername);
			const nextSocket = new WebSocket(wsURL.toString());
			ws = nextSocket;

			nextSocket.onopen = () => {
				if (ws !== nextSocket) {
					return;
				}
				wsState = 'open';
				clientLog('ws-open', { targetRoomId });
				reconnectAttempts = 0;
				markRoomAsRead(targetRoomId);
				flushPendingOutgoing(targetRoomId);
			};

			nextSocket.onmessage = (event: MessageEvent) => {
				if (ws !== nextSocket) {
					return;
				}
				if (typeof event.data !== 'string') {
					clientLog('ws-message-non-string', { targetRoomId, dataType: typeof event.data });
					return;
				}
				clientLog('ws-message-recv', { targetRoomId, bytes: event.data.length });
				handleSocketPayload(event.data, targetRoomId);
			};

			nextSocket.onerror = () => {
				if (ws !== nextSocket) {
					return;
				}
				wsState = 'error';
				clientLog('ws-error', { targetRoomId });
			};

			nextSocket.onclose = () => {
				if (ws !== nextSocket) {
					return;
				}
				wsState = 'closed';
				clientLog('ws-close', { targetRoomId });
				if (roomId === targetRoomId) {
					scheduleReconnect(targetRoomId);
				}
			};
		} catch (error) {
			console.error(`${CLIENT_LOG_PREFIX} socket connection failed`, error);
			wsState = 'error';
			scheduleReconnect(targetRoomId);
		}
	}

	function scheduleReconnect(targetRoomId: string) {
		clearReconnectTimer();
		reconnectAttempts = Math.min(reconnectAttempts + 1, 5);
		const delay = Math.min(1000 * 2 ** (reconnectAttempts - 1), 9000);
		clientLog('ws-reconnect-scheduled', { targetRoomId, reconnectAttempts, delayMs: delay });
		reconnectTimer = setTimeout(() => {
			if (roomId === targetRoomId) {
				connectToRoom(targetRoomId);
			}
		}, delay);
	}

	function closeSocket() {
		if (!ws) {
			wsRoomId = '';
			wsState = 'idle';
			return;
		}

		const activeSocket = ws;
		clientLog('ws-close-requested', { wsRoomId, readyState: activeSocket.readyState });
		ws = null;
		activeSocket.onopen = null;
		activeSocket.onmessage = null;
		activeSocket.onclose = null;
		activeSocket.onerror = null;
		if (
			activeSocket.readyState === WebSocket.OPEN ||
			activeSocket.readyState === WebSocket.CONNECTING
		) {
			activeSocket.close();
		}
		wsRoomId = '';
		wsState = 'idle';
	}

	function handleSocketPayload(raw: string, targetRoomId: string) {
		let parsed: unknown;
		try {
			parsed = JSON.parse(raw);
		} catch (error) {
			console.error(`${CLIENT_LOG_PREFIX} failed to parse socket payload`, error, raw);
			return;
		}

		if (Array.isArray(parsed)) {
			const history = parsed
				.map((entry) => parseIncomingMessage(entry, targetRoomId))
				.filter((entry): entry is ChatMessage => Boolean(entry));
			clientLog('ws-history-array', { targetRoomId, count: history.length });
			mergeMessages(targetRoomId, history);
			markRoomAsRead(targetRoomId);
			return;
		}

		if (isEnvelope(parsed)) {
			handleEnvelope(parsed, targetRoomId);
			return;
		}

		const singleMessage = parseIncomingMessage(parsed, targetRoomId);
		if (singleMessage) {
			clientLog('ws-single-message', { targetRoomId, messageId: singleMessage.id });
			addIncomingMessage(singleMessage);
		}
	}

	function isEnvelope(value: unknown): value is { type: string; payload: unknown } {
		return Boolean(
			value &&
			typeof value === 'object' &&
			'type' in value &&
			typeof (value as { type?: unknown }).type === 'string'
		);
	}

	function handleEnvelope(envelope: { type: string; payload: unknown }, targetRoomId: string) {
		const kind = envelope.type;
		clientLog('ws-envelope', { targetRoomId, kind });
		if (kind === 'history' || kind === 'recent_messages' || kind === 'initial_messages') {
			if (Array.isArray(envelope.payload)) {
				const history = envelope.payload
					.map((entry) => parseIncomingMessage(entry, targetRoomId))
					.filter((entry): entry is ChatMessage => Boolean(entry));
				mergeMessages(targetRoomId, history);
				markRoomAsRead(targetRoomId);
			}
			return;
		}

		if (kind === 'new_message') {
			const message = parseIncomingMessage(envelope.payload, targetRoomId);
			if (message) {
				addIncomingMessage(message);
			}
			return;
		}

		if (kind === 'online_list' && Array.isArray(envelope.payload)) {
			const members = envelope.payload
				.map((entry, index) => parseMember(entry, index))
				.filter((entry): entry is OnlineMember => Boolean(entry));
			onlineByRoom = {
				...onlineByRoom,
				[targetRoomId]: dedupeMembers(members)
			};
			return;
		}

		if (kind === 'user_joined') {
			const joined = parseMember(envelope.payload, Date.now());
			if (joined) {
				upsertOnlineMember(targetRoomId, joined);
			}
			return;
		}

		if (kind === 'user_left') {
			const leaving = parseMember(envelope.payload, Date.now());
			if (leaving) {
				removeOnlineMember(targetRoomId, leaving.id);
			}
		}
	}

	function parseIncomingMessage(value: unknown, fallbackRoomId: string): ChatMessage | null {
		if (!value || typeof value !== 'object') {
			return null;
		}

		const source = value as Record<string, unknown>;
		const nextRoomId = toStringValue(source.roomId ?? source.room_id ?? fallbackRoomId);
		if (!nextRoomId) {
			return null;
		}

		const messageType = toStringValue(source.type ?? 'text') || 'text';
		const messageContent = toStringValue(source.text ?? source.content ?? '');

		const normalized: ChatMessage = {
			id: toStringValue(source.id) || createMessageId(nextRoomId),
			roomId: nextRoomId,
			senderId: toStringValue(source.userId ?? source.senderId ?? source.sender_id ?? 'unknown'),
			senderName: toStringValue(
				source.username ?? source.senderName ?? source.sender_name ?? 'Unknown'
			),
			content: messageContent,
			type: messageType,
			createdAt: toTimestamp(
				source.time ?? source.createdAt ?? source.created_at ?? source.timestamp
			),
			pending: false
		};

		return normalized;
	}

	function parseMember(value: unknown, fallbackIndex: number): OnlineMember | null {
		if (!value || typeof value !== 'object') {
			return null;
		}
		const source = value as Record<string, unknown>;
		const memberId = toStringValue(
			source.id ?? source.userId ?? source.user_id ?? `member-${fallbackIndex}`
		);
		const memberName =
			toStringValue(source.name ?? source.username ?? source.userName ?? source.user_name) ||
			memberId;
		if (!memberId) {
			return null;
		}
		return { id: memberId, name: memberName, isOnline: true };
	}

	function addIncomingMessage(message: ChatMessage) {
		const shouldCountUnread = message.roomId !== roomId;
		upsertMessage(message.roomId, message, shouldCountUnread);
		if (message.roomId === roomId) {
			markRoomAsRead(roomId);
		}
	}

	function upsertMessage(targetRoomId: string, message: ChatMessage, shouldCountUnread: boolean) {
		const roomMessages = messagesByRoom[targetRoomId] ?? [];
		const existingIndex = roomMessages.findIndex((entry) => entry.id === message.id);

		let nextMessages: ChatMessage[];
		if (existingIndex >= 0) {
			nextMessages = [...roomMessages];
			nextMessages[existingIndex] = {
				...nextMessages[existingIndex],
				...message,
				pending: false
			};
		} else {
			nextMessages = [...roomMessages, message];
		}

		nextMessages.sort((a, b) => a.createdAt - b.createdAt);
		clientLog('message-upsert', {
			targetRoomId,
			messageId: message.id,
			pending: Boolean(message.pending),
			total: nextMessages.length
		});
		messagesByRoom = {
			...messagesByRoom,
			[targetRoomId]: nextMessages
		};

		updateThreadPreview(targetRoomId);

		if (shouldCountUnread) {
			roomThreads = sortThreads(
				roomThreads.map((thread) =>
					thread.id === targetRoomId ? { ...thread, unread: thread.unread + 1 } : thread
				)
			);
		}
	}

	function mergeMessages(targetRoomId: string, incoming: ChatMessage[]) {
		if (incoming.length === 0) {
			return;
		}

		const existing = messagesByRoom[targetRoomId] ?? [];
		const merged = new Map<string, ChatMessage>();
		for (const message of existing) {
			merged.set(message.id, message);
		}
		for (const message of incoming) {
			const current = merged.get(message.id);
			merged.set(message.id, { ...current, ...message, pending: false });
		}

		const nextMessages = [...merged.values()].sort((a, b) => a.createdAt - b.createdAt);
		messagesByRoom = {
			...messagesByRoom,
			[targetRoomId]: nextMessages
		};
		updateThreadPreview(targetRoomId);
	}

	function markRoomAsRead(targetRoomId: string) {
		if (!targetRoomId) {
			return;
		}
		roomThreads = sortThreads(
			roomThreads.map((thread) => (thread.id === targetRoomId ? { ...thread, unread: 0 } : thread))
		);
	}

	function upsertOnlineMember(targetRoomId: string, member: OnlineMember) {
		const members = onlineByRoom[targetRoomId] ?? [];
		const existingIndex = members.findIndex((entry) => entry.id === member.id);
		let next: OnlineMember[];
		if (existingIndex >= 0) {
			next = [...members];
			next[existingIndex] = { ...next[existingIndex], ...member, isOnline: true };
		} else {
			next = [...members, { ...member, isOnline: true }];
		}
		onlineByRoom = {
			...onlineByRoom,
			[targetRoomId]: dedupeMembers(next)
		};
	}

	function removeOnlineMember(targetRoomId: string, memberId: string) {
		const members = onlineByRoom[targetRoomId] ?? [];
		onlineByRoom = {
			...onlineByRoom,
			[targetRoomId]: members.filter((member) => member.id !== memberId)
		};
	}

	function dedupeMembers(members: OnlineMember[]) {
		const byId = new Map<string, OnlineMember>();
		for (const member of members) {
			byId.set(member.id, member);
		}
		return [...byId.values()];
	}

	function queueOutgoing(message: ChatMessage) {
		const currentQueue = pendingOutgoingByRoom[message.roomId] ?? [];
		clientLog('queue-outgoing', { roomId: message.roomId, messageId: message.id });
		pendingOutgoingByRoom = {
			...pendingOutgoingByRoom,
			[message.roomId]: [...currentQueue, message]
		};
	}

	function flushPendingOutgoing(targetRoomId: string) {
		const roomQueue = pendingOutgoingByRoom[targetRoomId] ?? [];
		if (roomQueue.length === 0 || !ws || ws.readyState !== WebSocket.OPEN) {
			clientLog('flush-outgoing-skip', {
				targetRoomId,
				queueSize: roomQueue.length,
				wsReadyState: ws?.readyState
			});
			return;
		}

		clientLog('flush-outgoing-start', { targetRoomId, queueSize: roomQueue.length });
		for (const queued of roomQueue) {
			clientLog('ws-send-queued', { targetRoomId, messageId: queued.id });
			ws.send(JSON.stringify(toWireMessage(queued)));
		}
		pendingOutgoingByRoom = {
			...pendingOutgoingByRoom,
			[targetRoomId]: []
		};
	}

	async function sendMessage() {
		if (!roomId) {
			clientLog('send-message-skip-no-room');
			return;
		}

		const text = draftMessage.trim();
		if (!text && !attachedFile) {
			clientLog('send-message-skip-empty');
			return;
		}

		const nextMessage: ChatMessage = {
			id: createMessageId(roomId),
			roomId,
			senderId: currentUserId,
			senderName: currentUsername,
			content: buildOutgoingContent(text, attachedFile),
			type: attachedFile ? 'file' : 'text',
			createdAt: Date.now(),
			pending: true
		};

		upsertMessage(roomId, nextMessage, false);
		markRoomAsRead(roomId);
		clientLog('send-message-local', {
			roomId,
			messageId: nextMessage.id,
			type: nextMessage.type,
			wsReadyState: ws?.readyState
		});

		draftMessage = '';
		attachedFile = null;
		if (fileInput) {
			fileInput.value = '';
		}

		if (ws && ws.readyState === WebSocket.OPEN) {
			clientLog('ws-send-live', { roomId, messageId: nextMessage.id });
			ws.send(JSON.stringify(toWireMessage(nextMessage)));
		} else {
			queueOutgoing(nextMessage);
		}

		await tick();
		scrollMessagesToBottom();
	}

	function toWireMessage(message: ChatMessage) {
		return {
			id: message.id,
			roomId: message.roomId,
			userId: message.senderId,
			username: message.senderName,
			text: message.content,
			time: new Date(message.createdAt).toISOString(),
			senderId: message.senderId,
			senderName: message.senderName,
			content: message.content,
			type: message.type,
			createdAt: new Date(message.createdAt).toISOString()
		};
	}

	function buildOutgoingContent(text: string, file: File | null) {
		if (!file) {
			return text;
		}
		if (!text) {
			return `[File] ${file.name}`;
		}
		return `[File] ${file.name} - ${text}`;
	}

	function onComposerKeyDown(event: KeyboardEvent) {
		if (event.key === 'Enter' && !event.shiftKey) {
			event.preventDefault();
			void sendMessage();
		}
	}

	function openFilePicker() {
		fileInput?.click();
	}

	function onFilePicked(event: Event) {
		const target = event.currentTarget as HTMLInputElement;
		const picked = target.files?.[0] ?? null;
		attachedFile = picked;
	}

	function removeAttachedFile() {
		attachedFile = null;
		if (fileInput) {
			fileInput.value = '';
		}
	}

	function toggleLeftMenu() {
		showLeftMenu = !showLeftMenu;
		showRoomMenu = false;
	}

	async function createRoomFromMenu() {
		const input = window.prompt('Enter a room name');
		showLeftMenu = false;
		if (!input) {
			return;
		}

		const requestedName = input.trim();
		if (!requestedName) {
			return;
		}

		const joinUsername = currentUsername || `Guest_${Math.floor(Math.random() * 10000)}`;
		if (!$currentUser) {
			currentUser.set({ id: currentUserId, username: joinUsername });
		}

		try {
			clientLog('api-rooms-join-request', { requestedName, joinUsername });
			const res = await fetch(`${API_BASE}/api/rooms/join`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ roomName: requestedName, username: joinUsername, type: 'ephemeral' })
			});
			const data = await res.json();
			clientLog('api-rooms-join-response', { status: res.status, ok: res.ok, data });
			if (!res.ok) {
				throw new Error(data.error || 'Failed to create room');
			}

			const nextRoomId = toStringValue(data.roomId) || toRoomSlug(requestedName);
			const nextRoomName = toStringValue(data.roomName) || formatRoomName(nextRoomId);
			const nextUserId = toStringValue(data.userId);
			if (!nextRoomId) {
				throw new Error('Failed to resolve room id');
			}
			if (nextUserId) {
				currentUser.set({ id: nextUserId, username: joinUsername });
			}

			ensureRoomThread(nextRoomId, nextRoomName);
			ensureOnlineSeed(nextRoomId);
			await goto(
				`/chat/${encodeURIComponent(nextRoomId)}?name=${encodeURIComponent(nextRoomName)}`
			);
		} catch (error) {
			clientLog('api-rooms-join-error', {
				error: error instanceof Error ? error.message : String(error)
			});
			const message = error instanceof Error ? error.message : 'Failed to create room';
			toastMessage = message;
			showToast = true;
			clearToastTimer();
			toastTimer = setTimeout(() => {
				showToast = false;
			}, 3000);
		}
	}

	async function extendRoomTTL(targetRoomId: string) {
		if (!browser || !targetRoomId) {
			return;
		}

		if (isExtendingRoom) {
			clientLog('api-rooms-extend-skipped', { roomId: targetRoomId, reason: 'in-flight' });
			return;
		}

		isExtendingRoom = true;
		try {
			clientLog('api-rooms-extend-request', { roomId: targetRoomId });
			const res = await fetch(`${API_BASE}/api/rooms/extend`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ roomId: targetRoomId })
			});
			const data = await res.json().catch(() => ({}));
			clientLog('api-rooms-extend-response', {
				roomId: targetRoomId,
				status: res.status,
				ok: res.ok,
				data
			});

			if (!res.ok) {
				toastMessage = data.error || 'Room has reached its 15-day limit';
				showToast = true;
				clearToastTimer();
				toastTimer = setTimeout(() => {
					showToast = false;
				}, 3000);
				return;
			}

			toastMessage = data.message || 'Room extended for 24 hours';
			showToast = true;
			clearToastTimer();
			toastTimer = setTimeout(() => {
				showToast = false;
			}, 3000);
		} catch (error) {
			console.error(`${CLIENT_LOG_PREFIX} failed to extend room TTL`, error);
			toastMessage = 'Failed to extend room';
			showToast = true;
			clearToastTimer();
			toastTimer = setTimeout(() => {
				showToast = false;
			}, 3000);
		} finally {
			isExtendingRoom = false;
		}
	}

	function requestRoomExtension() {
		if (!roomId) {
			return;
		}
		void extendRoomTTL(roomId);
	}

	function toggleRoomMenu() {
		showRoomMenu = !showRoomMenu;
		showLeftMenu = false;
	}

	function toggleRoomSearch() {
		showRoomSearch = !showRoomSearch;
		showRoomMenu = false;
		if (!showRoomSearch) {
			roomMessageSearch = '';
		}
	}

	function handleHeaderRoomClick() {
		showRoomInfo = true;
		showRoomMenu = false;
	}

	function closeRoomInfo() {
		showRoomInfo = false;
	}

	function clearCurrentRoomMessages() {
		if (!roomId) {
			return;
		}
		messagesByRoom = {
			...messagesByRoom,
			[roomId]: []
		};
		updateThreadPreview(roomId);
		showRoomMenu = false;
	}

	function getFilteredThreads(
		threads: ChatThread[],
		searchQuery: string,
		messageMap: Record<string, ChatMessage[]>,
		activeRoomId: string,
		activeRoomName: string
	) {
		const threadsWithActive = getThreadsWithActive(threads, activeRoomId, activeRoomName);
		const query = searchQuery.trim().toLowerCase();
		if (!query) {
			return threadsWithActive;
		}

		const filtered = threadsWithActive.filter((thread) => {
			if (thread.name.toLowerCase().includes(query)) {
				return true;
			}
			if (thread.lastMessage.toLowerCase().includes(query)) {
				return true;
			}
			const messages = messageMap[thread.id] ?? [];
			return messages.some(
				(message) =>
					message.content.toLowerCase().includes(query) ||
					message.senderName.toLowerCase().includes(query)
			);
		});

		if (activeRoomId && !filtered.some((thread) => thread.id === activeRoomId)) {
			const activeFallback = threadsWithActive.find((thread) => thread.id === activeRoomId);
			if (activeFallback) {
				return [activeFallback, ...filtered];
			}
		}

		return filtered;
	}

	function getThreadsWithActive(
		threads: ChatThread[],
		activeRoomId: string,
		activeRoomName: string
	) {
		if (!activeRoomId) {
			return threads;
		}
		if (threads.some((thread) => thread.id === activeRoomId)) {
			return threads;
		}
		return sortThreads([createThread(activeRoomId, activeRoomName), ...threads]);
	}

	function getVisibleMessages(messages: ChatMessage[], searchQuery: string) {
		const query = searchQuery.trim().toLowerCase();
		if (!query) {
			return messages;
		}

		return messages.filter(
			(message) =>
				message.content.toLowerCase().includes(query) ||
				message.senderName.toLowerCase().includes(query)
		);
	}

	function toRoomSlug(value: string) {
		const normalized = value.toLowerCase().trim();
		if (!normalized) {
			return '';
		}

		return normalized
			.replace(/[^a-z0-9\s_-]/g, '')
			.replace(/[\s_]+/g, '-')
			.replace(/-+/g, '-')
			.replace(/^-|-$/g, '');
	}

	function scrollMessagesToBottom() {
		if (!messageViewport) {
			return;
		}
		messageViewport.scrollTop = messageViewport.scrollHeight;
	}

	function getConnectionLabel(state: ConnectionState) {
		if (state === 'open') {
			return 'Live';
		}
		if (state === 'connecting') {
			return 'Connecting';
		}
		if (state === 'error') {
			return 'Error';
		}
		if (state === 'closed') {
			return 'Offline';
		}
		return 'Idle';
	}

	function createMessageId(targetRoomId: string) {
		if (browser && typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
			return crypto.randomUUID();
		}
		return `${targetRoomId}-${Date.now()}-${Math.floor(Math.random() * 1000000)}`;
	}

	function formatRoomName(targetRoomId: string) {
		return targetRoomId
			.split('-')
			.filter(Boolean)
			.map((part) => part.charAt(0).toUpperCase() + part.slice(1))
			.join(' ');
	}

	function formatClock(timestamp: number) {
		const safe = toTimestamp(timestamp);
		return new Date(safe).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
	}

	function formatLastSeen(timestamp: number) {
		const safe = toTimestamp(timestamp);
		return new Date(safe).toLocaleDateString([], {
			month: 'short',
			day: 'numeric'
		});
	}

	function toTimestamp(value: unknown) {
		if (typeof value === 'number' && Number.isFinite(value)) {
			return value;
		}
		if (typeof value === 'string') {
			const asNumber = Number(value);
			if (Number.isFinite(asNumber)) {
				return asNumber;
			}
			const parsed = Date.parse(value);
			if (Number.isFinite(parsed)) {
				return parsed;
			}
		}
		if (value instanceof Date) {
			return value.getTime();
		}
		return Date.now();
	}

	function toStringValue(value: unknown) {
		if (typeof value === 'string') {
			return value;
		}
		if (typeof value === 'number' || typeof value === 'boolean') {
			return String(value);
		}
		return '';
	}
</script>

{#if showToast}
	<div class="toast" role="status" aria-live="polite">
		{toastMessage}
	</div>
{/if}

<section class="chat-shell">
	<aside class="room-list">
		<div class="room-list-header">
			<div class="list-title">
				<h2>Chats</h2>
				<span class="thread-count">{roomThreads.length}</span>
			</div>
			<div class="list-actions">
				<button type="button" class="icon-button" on:click={toggleLeftMenu} title="Room options">
					...
				</button>
				{#if showLeftMenu}
					<div class="room-menu left-menu">
						<button type="button" on:click={createRoomFromMenu}>New room</button>
					</div>
				{/if}
			</div>
		</div>
		<div class="room-list-search">
			<input type="text" bind:value={chatListSearch} placeholder="Search names or messages" />
		</div>
		<div class="room-items">
			{#if filteredThreads.length === 0 && !roomId}
				<div class="empty-label">No chats matched your search.</div>
			{:else}
				{#each filteredThreads as thread (thread.id)}
					<button
						type="button"
						class="room-item {thread.id === roomId ? 'selected' : ''}"
						on:click={() => selectRoom(thread.id)}
					>
						<span class="avatar">{thread.name.charAt(0).toUpperCase()}</span>
						<span class="item-main">
							<span class="item-top">
								<span class="room-name">{thread.name}</span>
								<span class="room-time">{formatClock(thread.lastActivity)}</span>
							</span>
							<span class="item-bottom">
								<span class="room-preview">{thread.lastMessage || 'No messages yet'}</span>
								{#if thread.unread > 0}
									<span class="unread">{thread.unread}</span>
								{/if}
							</span>
						</span>
					</button>
				{/each}
			{/if}
		</div>
	</aside>

	<section class="chat-window">
		<header class="chat-header">
			<button type="button" class="room-title-button" on:click={handleHeaderRoomClick}>
				<span class="presence-dot"></span>
				<span class="title-text">
					<span class="title-main">{activeThread.name}</span>
					<span class="title-sub">
						{currentOnlineMembers.length} online
						{#if activeUnreadCount > 0}
							- {activeUnreadCount} unread
						{/if}
					</span>
				</span>
			</button>

			<div class="header-actions">
				<span class="connection {wsState}">{connectionLabel}</span>
				<button type="button" class="icon-button" on:click={toggleRoomMenu} title="More options">
					...
				</button>
				{#if showRoomMenu}
					<div class="room-menu">
						<button type="button" on:click={toggleRoomSearch}>
							{showRoomSearch ? 'Hide search' : 'Search messages'}
						</button>
						<button type="button" on:click={() => markRoomAsRead(roomId)}>Mark read</button>
						<button type="button" on:click={clearCurrentRoomMessages}>Clear local</button>
					</div>
				{/if}
			</div>
		</header>

		{#if showRoomSearch}
			<div class="chat-search-row">
				<input type="text" bind:value={roomMessageSearch} placeholder="Search in this room" />
			</div>
		{/if}

		<div class="messages" bind:this={messageViewport}>
			{#if visibleMessages.length === 0}
				<div class="empty-thread">
					{#if roomMessageSearch.trim()}
						No messages matched your room search.
					{:else}
						No messages yet. Send the first one.
					{/if}
				</div>
			{/if}

			{#each visibleMessages as message (message.id)}
				<article
					class="bubble {message.senderId === currentUserId ? 'mine' : 'theirs'} {message.pending
						? 'pending'
						: ''}"
				>
					<div class="bubble-meta">
						<span>{message.senderName}</span>
						<time>{formatClock(message.createdAt)}</time>
					</div>
					<div class="bubble-content">{message.content}</div>
				</article>
			{/each}
		</div>

		<footer class="composer">
			{#if attachedFile}
				<div class="attachment-pill">
					<span>{attachedFile.name}</span>
					<button type="button" on:click={removeAttachedFile}>x</button>
				</div>
			{/if}
			<div class="composer-row">
				<input
					bind:this={fileInput}
					type="file"
					class="hidden-file-input"
					on:change={onFilePicked}
				/>
				<button type="button" class="attach-button" on:click={openFilePicker}>Attach</button>
				<textarea
					bind:value={draftMessage}
					rows="1"
					placeholder="Type a message"
					on:keydown={onComposerKeyDown}
				></textarea>
				<button type="button" class="send-button" on:click={sendMessage}>Send</button>
			</div>
		</footer>
	</section>

	<aside class="online-panel">
		<div class="online-header">
			<h3>Online</h3>
			<span>{currentOnlineMembers.length}</span>
		</div>
		<div class="online-list">
			{#if currentOnlineMembers.length === 0}
				<div class="empty-label">No online members.</div>
			{:else}
				{#each currentOnlineMembers as member (member.id)}
					<div class="online-member">
						<span class="member-dot"></span>
						<span class="member-name">{member.name}</span>
					</div>
				{/each}
			{/if}
		</div>
	</aside>
</section>

{#if showRoomInfo}
	<button
		type="button"
		class="mobile-info-backdrop"
		aria-label="Close room info"
		on:click={closeRoomInfo}
	></button>
	<aside class="mobile-info-panel">
		<header>
			<h3>{activeThread.name}</h3>
			<button type="button" on:click={closeRoomInfo}>Close</button>
		</header>
		<div class="mobile-info-content">
			<div class="room-actions">
				<button
					type="button"
					class="extend-room-button"
					on:click={requestRoomExtension}
					disabled={isExtendingRoom}
				>
					{isExtendingRoom ? 'Extending...' : 'Extend Room (24h)'}
				</button>
				<p>Manually extends this room for 24 hours (up to 15 days total).</p>
			</div>

			{#if currentOnlineMembers.length === 0}
				<div class="empty-label">No online members.</div>
			{:else}
				{#each currentOnlineMembers as member (member.id)}
					<div class="online-member">
						<span class="member-dot"></span>
						<div>
							<div class="member-name">{member.name}</div>
							<div class="member-meta">Seen {formatLastSeen(Date.now())}</div>
						</div>
					</div>
				{/each}
			{/if}
		</div>
	</aside>
{/if}

<style>
	.chat-shell {
		height: calc(100vh - 72px);
		min-height: 620px;
		display: grid;
		grid-template-columns: 320px minmax(0, 1fr) 270px;
		border-top: 1px solid #d9dee4;
		background: #f3f5f7;
	}

	.room-list {
		display: flex;
		flex-direction: column;
		border-right: 1px solid #d9dee4;
		background: #ffffff;
	}

	.room-list-header {
		padding: 1rem 1rem 0.75rem;
		display: flex;
		justify-content: space-between;
		align-items: center;
		position: relative;
	}

	.list-title {
		display: flex;
		align-items: center;
		gap: 0.5rem;
	}

	.list-actions {
		position: relative;
	}

	.room-list-header h2 {
		margin: 0;
		font-size: 1.05rem;
	}

	.thread-count {
		font-size: 0.85rem;
		font-weight: 700;
		color: #166534;
		background: #dcfce7;
		padding: 0.2rem 0.5rem;
		border-radius: 999px;
	}

	.room-list-search {
		padding: 0 1rem 0.75rem;
	}

	.room-list-search input {
		width: 100%;
		border: 1px solid #cfd8e3;
		border-radius: 8px;
		padding: 0.55rem 0.7rem;
		font-size: 0.92rem;
	}

	.room-items {
		flex: 1;
		overflow: auto;
	}

	.room-item {
		width: 100%;
		display: flex;
		gap: 0.75rem;
		padding: 0.78rem 0.9rem;
		border: none;
		border-top: 1px solid #f1f3f6;
		text-align: left;
		background: transparent;
		cursor: pointer;
	}

	.room-item:hover {
		background: #f8fafc;
	}

	.room-item.selected {
		background: #e8f5ec;
	}

	.avatar {
		width: 38px;
		height: 38px;
		border-radius: 50%;
		background: #dde7f4;
		color: #1e293b;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		font-weight: 700;
	}

	.item-main {
		min-width: 0;
		flex: 1;
		display: flex;
		flex-direction: column;
		gap: 0.35rem;
	}

	.item-top,
	.item-bottom {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 0.6rem;
	}

	.room-name {
		font-size: 0.92rem;
		font-weight: 600;
		color: #162136;
	}

	.room-time {
		font-size: 0.78rem;
		color: #607188;
		white-space: nowrap;
	}

	.room-preview {
		font-size: 0.82rem;
		color: #546479;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.unread {
		min-width: 20px;
		height: 20px;
		border-radius: 999px;
		background: #16a34a;
		color: #ffffff;
		font-size: 0.75rem;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		font-weight: 700;
	}

	.chat-window {
		display: flex;
		flex-direction: column;
		min-width: 0;
		background: #efeae2;
	}

	.chat-header {
		position: relative;
		background: #f6f8fa;
		border-bottom: 1px solid #d9dee4;
		padding: 0.8rem 1rem;
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 1rem;
	}

	.room-title-button {
		border: none;
		background: transparent;
		padding: 0;
		display: flex;
		align-items: center;
		gap: 0.55rem;
		cursor: pointer;
		color: #0f172a;
	}

	.presence-dot {
		width: 10px;
		height: 10px;
		border-radius: 50%;
		background: #22c55e;
	}

	.title-text {
		display: inline-flex;
		flex-direction: column;
		align-items: flex-start;
	}

	.title-main {
		font-size: 0.98rem;
		font-weight: 700;
	}

	.title-sub {
		font-size: 0.76rem;
		color: #64748b;
	}

	.header-actions {
		display: flex;
		align-items: center;
		gap: 0.45rem;
		position: relative;
	}

	.connection {
		font-size: 0.74rem;
		font-weight: 700;
		padding: 0.2rem 0.5rem;
		border-radius: 999px;
		background: #dbe5f1;
		color: #1e293b;
	}

	.connection.open {
		background: #dcfce7;
		color: #166534;
	}

	.connection.connecting {
		background: #fef9c3;
		color: #854d0e;
	}

	.connection.error,
	.connection.closed {
		background: #fee2e2;
		color: #b91c1c;
	}

	.icon-button {
		border: 1px solid #cdd7e1;
		background: #ffffff;
		border-radius: 6px;
		padding: 0.35rem 0.55rem;
		font-size: 0.78rem;
		cursor: pointer;
	}

	.room-menu {
		position: absolute;
		top: calc(100% + 6px);
		right: 0;
		background: #ffffff;
		border: 1px solid #d8e0e9;
		border-radius: 8px;
		box-shadow: 0 8px 20px rgba(15, 23, 42, 0.12);
		overflow: hidden;
		min-width: 138px;
		z-index: 100;
	}

	.left-menu {
		left: 0;
		right: auto;
	}

	.room-menu button {
		width: 100%;
		border: none;
		background: #ffffff;
		padding: 0.55rem 0.75rem;
		text-align: left;
		font-size: 0.84rem;
		cursor: pointer;
	}

	.room-menu button:hover {
		background: #f3f6fa;
	}

	.chat-search-row {
		padding: 0.65rem 0.9rem;
		background: #f6f8fa;
		border-bottom: 1px solid #d9dee4;
	}

	.chat-search-row input {
		width: 100%;
		border: 1px solid #cfd8e3;
		border-radius: 8px;
		padding: 0.55rem 0.7rem;
		font-size: 0.9rem;
	}

	.messages {
		flex: 1;
		overflow: auto;
		padding: 1rem;
		display: flex;
		flex-direction: column;
		gap: 0.72rem;
	}

	.bubble {
		max-width: min(75%, 540px);
		border-radius: 12px;
		padding: 0.58rem 0.7rem;
		background: #ffffff;
		box-shadow: 0 1px 2px rgba(15, 23, 42, 0.08);
	}

	.bubble.mine {
		align-self: flex-end;
		background: #dcf8c6;
	}

	.bubble.pending {
		opacity: 0.65;
	}

	.bubble-meta {
		display: flex;
		justify-content: space-between;
		gap: 0.75rem;
		font-size: 0.72rem;
		color: #5b6472;
		margin-bottom: 0.28rem;
	}

	.bubble-content {
		font-size: 0.89rem;
		line-height: 1.35;
		color: #142032;
		white-space: pre-wrap;
		word-break: break-word;
	}

	.composer {
		border-top: 1px solid #d9dee4;
		background: #f6f8fa;
		padding: 0.75rem;
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
	}

	.attachment-pill {
		display: inline-flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.35rem 0.6rem;
		background: #dbeafe;
		color: #1e3a8a;
		border-radius: 999px;
		width: fit-content;
		font-size: 0.82rem;
	}

	.attachment-pill button {
		border: none;
		background: transparent;
		color: inherit;
		cursor: pointer;
		font-weight: 700;
	}

	.composer-row {
		display: grid;
		grid-template-columns: auto 1fr auto;
		gap: 0.55rem;
		align-items: end;
	}

	.hidden-file-input {
		display: none;
	}

	.attach-button,
	.send-button {
		border: 1px solid #cfd8e3;
		background: #ffffff;
		border-radius: 8px;
		padding: 0.52rem 0.72rem;
		font-size: 0.85rem;
		cursor: pointer;
	}

	.send-button {
		background: #1f9d4c;
		border-color: #1f9d4c;
		color: #ffffff;
	}

	textarea {
		width: 100%;
		resize: none;
		min-height: 40px;
		max-height: 110px;
		border: 1px solid #cfd8e3;
		border-radius: 9px;
		padding: 0.55rem 0.66rem;
		font-size: 0.91rem;
		font-family: inherit;
	}

	.online-panel {
		border-left: 1px solid #d9dee4;
		background: #ffffff;
		display: flex;
		flex-direction: column;
	}

	.online-header {
		padding: 1rem;
		display: flex;
		justify-content: space-between;
		align-items: center;
		border-bottom: 1px solid #edf1f5;
	}

	.online-header h3 {
		margin: 0;
		font-size: 0.95rem;
	}

	.online-header span {
		font-size: 0.82rem;
		color: #334155;
		font-weight: 700;
	}

	.online-list {
		flex: 1;
		overflow: auto;
		padding: 0.75rem;
	}

	.online-member {
		display: flex;
		align-items: center;
		gap: 0.52rem;
		padding: 0.45rem 0.2rem;
	}

	.member-dot {
		width: 9px;
		height: 9px;
		border-radius: 50%;
		background: #22c55e;
	}

	.member-name {
		font-size: 0.88rem;
		color: #172132;
	}

	.member-meta {
		font-size: 0.75rem;
		color: #64748b;
	}

	.empty-label,
	.empty-thread {
		color: #64748b;
		font-size: 0.84rem;
		padding: 1rem;
	}

	.mobile-info-backdrop {
		position: fixed;
		inset: 0;
		background: rgba(15, 23, 42, 0.35);
		border: none;
		z-index: 150;
	}

	.mobile-info-panel {
		position: fixed;
		right: 0;
		top: 0;
		height: 100vh;
		width: min(92vw, 320px);
		background: #ffffff;
		z-index: 160;
		box-shadow: -14px 0 30px rgba(15, 23, 42, 0.2);
		display: flex;
		flex-direction: column;
	}

	.mobile-info-panel header {
		padding: 0.9rem 1rem;
		border-bottom: 1px solid #e8edf3;
		display: flex;
		justify-content: space-between;
		align-items: center;
	}

	.mobile-info-panel header h3 {
		margin: 0;
		font-size: 1rem;
	}

	.mobile-info-panel header button {
		border: 1px solid #d4dce6;
		background: #ffffff;
		border-radius: 7px;
		padding: 0.32rem 0.5rem;
		cursor: pointer;
	}

	.mobile-info-content {
		padding: 0.7rem 0.85rem;
		overflow: auto;
	}

	.room-actions {
		margin-bottom: 0.9rem;
		padding: 0.75rem;
		border: 1px solid #e2e8f0;
		border-radius: 10px;
		background: #f8fafc;
	}

	.room-actions p {
		margin: 0.45rem 0 0;
		font-size: 0.78rem;
		color: #475569;
	}

	.extend-room-button {
		width: 100%;
		border: 1px solid #15803d;
		background: #16a34a;
		color: #ffffff;
		border-radius: 8px;
		padding: 0.48rem 0.65rem;
		font-size: 0.84rem;
		font-weight: 600;
		cursor: pointer;
	}

	.extend-room-button:disabled {
		opacity: 0.7;
		cursor: not-allowed;
	}

	.toast {
		position: fixed;
		top: 0.8rem;
		left: 50%;
		transform: translateX(-50%);
		background: #1f2937;
		color: #ffffff;
		padding: 0.65rem 1rem;
		border-radius: 999px;
		font-size: 0.87rem;
		font-weight: 600;
		box-shadow: 0 12px 24px rgba(0, 0, 0, 0.22);
		z-index: 500;
		pointer-events: none;
	}

	@media (max-width: 1199px) {
		.chat-shell {
			grid-template-columns: 290px minmax(0, 1fr);
		}

		.online-panel {
			display: none;
		}
	}

	@media (max-width: 900px) {
		.chat-shell {
			grid-template-columns: 1fr;
			grid-template-rows: minmax(220px, 36%) minmax(0, 64%);
			height: calc(100vh - 72px);
		}

		.room-list {
			display: flex;
			border-right: none;
			border-bottom: 1px solid #d9dee4;
		}

		.chat-window {
			min-height: 0;
		}
	}
</style>
