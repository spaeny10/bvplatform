'use client';

// Inline camera-name edit hook extracted from VideoPlayer (P1-B-11 session 18).
// Owns the 3 useState/useRef slots (editingName, editValue, nameInputRef)
// and the start/commit/cancel handlers. The hook stays a no-op when
// `isAdmin` is false or `onRename` is missing — callers can always wire
// the handlers and they'll just bail.

import { useEffect, useRef, useState } from 'react';

export interface CameraNameEdit {
    editingName: boolean;
    editValue: string;
    setEditValue: (v: string) => void;
    nameInputRef: React.RefObject<HTMLInputElement>;
    startEditing: (e: React.MouseEvent) => void;
    commitRename: () => void;
    cancelRename: () => void;
}

export function useCameraNameEdit(
    cameraId: string,
    cameraName: string,
    isAdmin: boolean,
    onRename: ((cameraId: string, newName: string) => void) | undefined,
): CameraNameEdit {
    const [editingName, setEditingName] = useState(false);
    const [editValue, setEditValue] = useState(cameraName);
    const nameInputRef = useRef<HTMLInputElement>(null);

    useEffect(() => { setEditValue(cameraName); }, [cameraName]);

    const startEditing = (e: React.MouseEvent) => {
        if (!isAdmin || !onRename) return;
        e.stopPropagation();
        e.preventDefault();
        setEditValue(cameraName);
        setEditingName(true);
        setTimeout(() => nameInputRef.current?.select(), 0);
    };

    const commitRename = () => {
        const trimmed = editValue.trim();
        setEditingName(false);
        if (trimmed && trimmed !== cameraName && onRename) {
            onRename(cameraId, trimmed);
        }
    };

    const cancelRename = () => {
        setEditingName(false);
        setEditValue(cameraName);
    };

    return { editingName, editValue, setEditValue, nameInputRef, startEditing, commitRename, cancelRename };
}
