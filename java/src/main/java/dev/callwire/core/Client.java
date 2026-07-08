package dev.callwire.core;

import java.io.*;
import java.net.Socket;
import java.util.*;
import java.util.concurrent.*;

class TimeoutException extends Exception {
    TimeoutException(String msg) {
        super(msg);
    }
}

/**
 * Callwire client: thread-safe, bidirectional RPC over a single TCP connection.
 * Supports unary, server-streaming, client-streaming, and bidi-streaming.
 */
public class Client {

    private Socket socket;
    private final Object writeLock = new Object();
    private final Map<Long, BlockingQueue<Map<String, Object>>> pending = new ConcurrentHashMap<>();
    private long nextId = 0;
    private final Object idLock = new Object();
    private volatile boolean connected = false;
    private Thread readThread;

    public void connect(String host, int port) throws IOException {
        socket = new Socket(host, port);
        connected = true;
        readThread = new Thread(this::readLoop);
        readThread.setDaemon(true);
        readThread.start();
    }

    public void close() throws IOException {
        connected = false;
        if (socket != null) {
            socket.close();
        }
        if (readThread != null) {
            try {
                readThread.join(2000);
            } catch (InterruptedException e) {
                // ignore
            }
        }
    }

    /**
     * Unary call: send request, wait for response.
     */
    public Object call(String func, List<Object> args) throws IOException, CallwireException, TimeoutException {
        long id;
        synchronized (idLock) {
            id = ++nextId;
        }

        BlockingQueue<Map<String, Object>> respQueue = new LinkedBlockingQueue<>(1);
        pending.put(id, respQueue);

        try {
            byte[] payload = Codec.encodeRequest(id, func, args);
            synchronized (writeLock) {
                Framing.writeFrame(socket, payload);
            }

            Map<String, Object> msg = respQueue.poll(30, TimeUnit.SECONDS);
            if (msg == null) {
                throw new TimeoutException("RPC call timeout");
            }

            String type = (String) msg.get("type");
            if ("error".equals(type)) {
                throw new CallwireException((String) msg.get("error_type"), (String) msg.get("message"));
            }

            return msg.get("result");
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new IOException("RPC call interrupted");
        } finally {
            pending.remove(id);
        }
    }

    /**
     * Server-streaming: send request, iterate over stream_chunk responses until stream_end.
     */
    public Iterator<Object> callStream(String func, List<Object> args) throws IOException {
        long id;
        synchronized (idLock) {
            id = ++nextId;
        }

        BlockingQueue<Map<String, Object>> respQueue = new LinkedBlockingQueue<>(256);
        pending.put(id, respQueue);

        try {
            byte[] payload = Codec.encodeRequest(id, func, args);
            synchronized (writeLock) {
                Framing.writeFrame(socket, payload);
            }

            return new StreamingIterator(respQueue, id, pending);
        } catch (IOException e) {
            pending.remove(id);
            throw e;
        }
    }

    /**
     * Client-streaming: send multiple chunks, receive single response.
     */
    public ExportStream exportStream(String func) throws IOException {
        long id;
        synchronized (idLock) {
            id = ++nextId;
        }

        BlockingQueue<Map<String, Object>> respQueue = new LinkedBlockingQueue<>(1);
        pending.put(id, respQueue);

        try {
            byte[] payload = Codec.encodeRequest(id, func, new ArrayList<>());
            synchronized (writeLock) {
                Framing.writeFrame(socket, payload);
            }

            return new ExportStream(this, id, respQueue, pending, writeLock);
        } catch (IOException e) {
            pending.remove(id);
            throw e;
        }
    }

    /**
     * Bidirectional-streaming: send and receive chunks concurrently.
     */
    public BidiStream importBidi(String func) throws IOException {
        long id;
        synchronized (idLock) {
            id = ++nextId;
        }

        BlockingQueue<Map<String, Object>> queue = new LinkedBlockingQueue<>(256);
        pending.put(id, queue);

        try {
            byte[] payload = Codec.encodeRequest(id, func, new ArrayList<>(), true);
            synchronized (writeLock) {
                Framing.writeFrame(socket, payload);
            }

            return new BidiStream(this, id, queue, pending, writeLock);
        } catch (IOException e) {
            pending.remove(id);
            throw e;
        }
    }

    void writeFrame(byte[] payload) throws IOException {
        synchronized (writeLock) {
            Framing.writeFrame(socket, payload);
        }
    }

    private void readLoop() {
        try {
            while (connected) {
                byte[] payload = Framing.readFrame(socket);
                Map<String, Object> msg = Codec.decode(payload);
                long msgId = ((Number) msg.get("id")).longValue();
                String type = (String) msg.get("type");

                BlockingQueue<Map<String, Object>> queue = pending.get(msgId);
                if (queue != null) {
                    queue.offer(msg);
                    // Keep queue in pending for stream_chunk; remove for stream_end, response, error
                    if (!"stream_chunk".equals(type)) {
                        pending.remove(msgId);
                    }
                }
            }
        } catch (IOException e) {
            connected = false;
        }
    }

    private static class StreamingIterator implements Iterator<Object> {
        private final BlockingQueue<Map<String, Object>> queue;
        private final long id;
        private final Map<Long, BlockingQueue<Map<String, Object>>> pending;
        private boolean done = false;
        private Map<String, Object> nextMsg;

        StreamingIterator(BlockingQueue<Map<String, Object>> queue, long id,
                         Map<Long, BlockingQueue<Map<String, Object>>> pending) {
            this.queue = queue;
            this.id = id;
            this.pending = pending;
            advance();
        }

        private void advance() {
            try {
                nextMsg = queue.poll(30, TimeUnit.SECONDS);
                if (nextMsg == null) {
                    done = true;
                    pending.remove(id);
                    return;
                }

                String type = (String) nextMsg.get("type");
                if ("stream_end".equals(type) || "error".equals(type)) {
                    done = true;
                    pending.remove(id);
                }
            } catch (InterruptedException e) {
                done = true;
                pending.remove(id);
            }
        }

        @Override
        public boolean hasNext() {
            return !done && nextMsg != null;
        }

        @Override
        public Object next() {
            if (!hasNext()) {
                throw new NoSuchElementException();
            }
            Object result = nextMsg.get("result");
            advance();
            return result;
        }
    }
}

class ExportStream {
    private final Client client;
    private final long id;
    private final BlockingQueue<Map<String, Object>> respQueue;
    private final Map<Long, BlockingQueue<Map<String, Object>>> pending;

    ExportStream(Client client, long id, BlockingQueue<Map<String, Object>> respQueue,
                 Map<Long, BlockingQueue<Map<String, Object>>> pending, Object writeLock) {
        this.client = client;
        this.id = id;
        this.respQueue = respQueue;
        this.pending = pending;
    }

    public void send(Object chunk) throws IOException {
        byte[] payload = Codec.encodeStreamChunk(id, chunk);
        client.writeFrame(payload);
    }

    public Object closeAndRecv() throws IOException, CallwireException, TimeoutException {
        byte[] payload = Codec.encodeStreamClose(id);
        client.writeFrame(payload);

        try {
            Map<String, Object> msg = respQueue.poll(30, TimeUnit.SECONDS);
            if (msg == null) {
                throw new TimeoutException("closeAndRecv timeout");
            }

            String type = (String) msg.get("type");
            if ("error".equals(type)) {
                throw new CallwireException((String) msg.get("error_type"), (String) msg.get("message"));
            }

            return msg.get("result");
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new IOException("closeAndRecv interrupted");
        } finally {
            pending.remove(id);
        }
    }
}

class BidiStream {
    private final Client client;
    private final long id;
    private final BlockingQueue<Map<String, Object>> queue;
    private final Map<Long, BlockingQueue<Map<String, Object>>> pending;
    private boolean sentEnd = false;

    BidiStream(Client client, long id, BlockingQueue<Map<String, Object>> queue,
               Map<Long, BlockingQueue<Map<String, Object>>> pending, Object writeLock) {
        this.client = client;
        this.id = id;
        this.queue = queue;
        this.pending = pending;
    }

    public void send(Object chunk) throws IOException {
        byte[] payload = Codec.encodeStreamChunk(id, chunk);
        client.writeFrame(payload);
    }

    public void closeSend() throws IOException {
        if (sentEnd) return;
        sentEnd = true;
        byte[] payload = Codec.encodeStreamEnd(id);
        client.writeFrame(payload);
    }

    public Object recv() throws IOException, CallwireException, TimeoutException {
        try {
            Map<String, Object> msg = queue.poll(30, TimeUnit.SECONDS);
            if (msg == null) {
                throw new TimeoutException("recv timeout");
            }

            String type = (String) msg.get("type");
            if ("stream_chunk".equals(type)) {
                return msg.get("result");
            } else if ("stream_end".equals(type)) {
                pending.remove(id);
                return null;
            } else if ("error".equals(type)) {
                pending.remove(id);
                throw new CallwireException((String) msg.get("error_type"), (String) msg.get("message"));
            }
            return null;
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new IOException("recv interrupted");
        }
    }
}

class CallwireException extends Exception {
    public final String errorType;

    public CallwireException(String errorType, String message) {
        super(message);
        this.errorType = errorType;
    }
}
