export interface Room {
    id: string;
    name: string;
    participants: number;
    expiresAt: number | null;
    type: 'ephemeral' | 'persistent';
}

export interface IncomingEvent {
    type: 'new_message' | 'user_joined' | 'user_left' | 'room_expired';
    payload: any;
}

export interface User {
    id: string;
    username: string;
    avatarUrl?: string;
}

export interface Message {
    id: string;
    text: string;
    senderId: string;
    timestamp: number;
    roomId: string;
}
