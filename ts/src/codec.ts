import { encode, decode } from '@msgpack/msgpack';

export interface WireMessage {
  id: number;
  type: string;
  func?: string;
  args?: unknown[];
  stream?: boolean;
  result?: unknown;
  error_type?: string;
  message?: string;
}

/**
 * Pack a request message into a MessagePack buffer.
 */
export function packRequest(id: number, func: string, args: unknown[]): Buffer {
  const msg: WireMessage = { id, type: 'request', func, args };
  return Buffer.from(encode(msg));
}

/**
 * Pack a response message.
 */
export function packResponse(id: number, result: unknown): Buffer {
  const msg: WireMessage = { id, type: 'response', result };
  return Buffer.from(encode(msg));
}

/**
 * Pack an error response message.
 */
export function packError(id: number, errorType: string, message: string): Buffer {
  const msg: WireMessage = { id, type: 'error', error_type: errorType, message };
  return Buffer.from(encode(msg));
}

/**
 * Pack a stream chunk message.
 */
export function packStreamChunk(id: number, result: unknown): Buffer {
  const msg: WireMessage = { id, type: 'stream_chunk', result };
  return Buffer.from(encode(msg));
}

/**
 * Pack a stream_end sentinel.
 */
export function packStreamEnd(id: number): Buffer {
  const msg: WireMessage = { id, type: 'stream_end' };
  return Buffer.from(encode(msg));
}

/**
 * Pack a stream_close sentinel (client-streaming termination).
 */
export function packStreamClose(id: number): Buffer {
  const msg: WireMessage = { id, type: 'stream_close' };
  return Buffer.from(encode(msg));
}

/**
 * Pack a bidirectional-streaming request.
 */
export function packBidiRequest(id: number, func: string, args: unknown[]): Buffer {
  const msg: WireMessage = { id, type: 'request', func, args, stream: true };
  return Buffer.from(encode(msg));
}

/**
 * Decode a raw buffer into a WireMessage.
 */
export function unpack(payload: Buffer): WireMessage {
  const decoded = decode(payload) as Record<string, unknown>;
  return {
    id: decoded.id as number,
    type: decoded.type as string,
    func: decoded.func as string | undefined,
    args: decoded.args as unknown[] | undefined,
    stream: decoded.stream as boolean | undefined,
    result: decoded.result,
    error_type: decoded.error_type as string | undefined,
    message: decoded.message as string | undefined,
  };
}
