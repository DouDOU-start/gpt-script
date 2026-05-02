import { useState } from 'react';
import type { MessageType } from '../types';

export function useMessage() {
  const [message, setMessage] = useState('');
  const [messageType, setMessageType] = useState<MessageType>('default');

  function showMessage(value: unknown, type: MessageType) {
    setMessage(value instanceof Error ? value.message : String(value));
    setMessageType(type);
  }

  return { message, messageType, showMessage };
}
