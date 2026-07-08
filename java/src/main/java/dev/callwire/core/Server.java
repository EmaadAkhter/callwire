package dev.callwire.core;

import java.io.*;
import java.net.*;
import java.util.*;
import java.util.concurrent.*;

/**
 * Callwire server: accepts connections, dispatches to registered handlers.
 * Each handler is a lambda or functional interface that receives args and returns a result.
 */
public class Server {

    private static class StreamMarker {
        final Iterator<Object> iter;
        StreamMarker(Iterator<Object> iter) { this.iter = iter; }
    }

    private static class ClientStreamMarker {
        final ClientStreamHandler handler;
        ClientStreamMarker(ClientStreamHandler handler) { this.handler = handler; }
    }

    private static class BidiMarker {
        final BidiHandler handler;
        BidiMarker(BidiHandler handler) { this.handler = handler; }
    }

    /** Sentinel placed on a streaming-input queue to signal end of input. */
    private static final Object STREAM_INPUT_END = new Object();

    private final Map<String, Object> registry = new ConcurrentHashMap<>();
    private ServerSocket serverSocket;
    private volatile boolean running = false;
    private ExecutorService threadPool = Executors.newFixedThreadPool(16);
    private ExecutorService streamWorkers = Executors.newCachedThreadPool();

    public interface Handler {
        Object handle(List<Object> args) throws Exception;
    }

    public interface StreamHandler {
        Iterator<Object> handle(List<Object> args) throws Exception;
    }

    /** Client-streaming: receives an iterator of incoming chunks, returns a single response. */
    public interface ClientStreamHandler {
        Object handle(Iterator<Object> chunks) throws Exception;
    }

    /** Bidi-streaming: receives an iterator of incoming chunks, returns an iterator of outgoing chunks. */
    public interface BidiHandler {
        Iterator<Object> handle(Iterator<Object> chunks) throws Exception;
    }

    public void export(String funcName, Handler handler) {
        registry.put(funcName, handler);
    }

    public void exportStream(String funcName, StreamHandler handler) {
        registry.put(funcName, (Handler) args -> new StreamMarker(handler.handle(args)));
    }

    public void exportClientStream(String funcName, ClientStreamHandler handler) {
        registry.put(funcName, new ClientStreamMarker(handler));
    }

    public void exportBidi(String funcName, BidiHandler handler) {
        registry.put(funcName, new BidiMarker(handler));
    }

    public void serve(String host, int port) throws IOException {
        serverSocket = new ServerSocket();
        serverSocket.setReuseAddress(true);
        serverSocket.bind(new InetSocketAddress(host, port));
        running = true;

        System.out.println("Callwire server listening on " + host + ":" + port);

        try {
            while (running) {
                Socket conn = serverSocket.accept();
                threadPool.execute(() -> handleConnection(conn));
            }
        } finally {
            serverSocket.close();
            threadPool.shutdown();
        }
    }

    private void handleConnection(Socket conn) {
        // Tracks in-progress client-streaming/bidi calls: id -> input queue.
        Map<Long, BlockingQueue<Object>> streamInputs = new ConcurrentHashMap<>();
        try {
            while (running) {
                byte[] payload = Framing.readFrame(conn);
                Map<String, Object> msg = Codec.decode(payload);
                String type = (String) msg.get("type");
                long id = ((Number) msg.get("id")).longValue();

                if ("stream_chunk".equals(type) || "stream_close".equals(type) || "stream_end".equals(type)) {
                    BlockingQueue<Object> q = streamInputs.get(id);
                    if (q != null) {
                        if ("stream_chunk".equals(type)) {
                            q.offer(msg.get("result"));
                        } else {
                            q.offer(STREAM_INPUT_END);
                            streamInputs.remove(id);
                        }
                    }
                    continue;
                }

                dispatch(conn, msg, streamInputs);
            }
        } catch (EOFException e) {
            // Connection closed
        } catch (IOException e) {
            // Log error
        } finally {
            // Unblock any handler still waiting on input from a call that never
            // sent stream_close/stream_end before the connection dropped.
            for (BlockingQueue<Object> q : streamInputs.values()) {
                q.offer(STREAM_INPUT_END);
            }
            streamInputs.clear();
            try {
                conn.close();
            } catch (IOException e) {
                // ignore
            }
        }
    }

    private void dispatch(Socket conn, Map<String, Object> msg, Map<Long, BlockingQueue<Object>> streamInputs) throws IOException {
        long id = ((Number) msg.get("id")).longValue();
        String type = (String) msg.get("type");

        if (!"request".equals(type)) {
            return; // ignore non-request messages
        }

        String func = (String) msg.get("func");
        Object argsObj = msg.get("args");
        List<Object> args = argsObj instanceof List ? (List<Object>) argsObj : new ArrayList<>();
        boolean isBidi = Boolean.TRUE.equals(msg.get("stream"));

        Object entry = registry.get(func);
        if (entry == null) {
            byte[] error = Codec.encodeError(id, "NotFoundError",
                    "Function '" + func + "' not exported");
            synchronized (conn) {
                Framing.writeFrame(conn, error);
            }
            return;
        }

        if (entry instanceof ClientStreamMarker && !isBidi) {
            BlockingQueue<Object> q = new LinkedBlockingQueue<>();
            streamInputs.put(id, q);
            ClientStreamHandler handler = ((ClientStreamMarker) entry).handler;
            streamWorkers.execute(() -> runClientStreamHandler(conn, id, handler, q));
            return;
        }

        if (entry instanceof BidiMarker && isBidi) {
            BlockingQueue<Object> q = new LinkedBlockingQueue<>();
            streamInputs.put(id, q);
            BidiHandler handler = ((BidiMarker) entry).handler;
            streamWorkers.execute(() -> runBidiHandler(conn, id, handler, q));
            return;
        }

        if (!(entry instanceof Handler)) {
            byte[] error = Codec.encodeError(id, "TypeError",
                    "Function '" + func + "' registered for a different call pattern");
            synchronized (conn) {
                Framing.writeFrame(conn, error);
            }
            return;
        }

        Handler handler = (Handler) entry;
        try {
            Object result = handler.handle(args);

            // Check if result is a stream (wrapped in StreamMarker)
            if (result instanceof StreamMarker) {
                Iterator<Object> iter = ((StreamMarker) result).iter;
                while (iter.hasNext()) {
                    Object chunk = iter.next();
                    byte[] payload = Codec.encodeStreamChunk(id, chunk);
                    synchronized (conn) {
                        Framing.writeFrame(conn, payload);
                    }
                }
                byte[] end = Codec.encodeStreamEnd(id);
                synchronized (conn) {
                    Framing.writeFrame(conn, end);
                }
            } else {
                // Unary response
                byte[] response = Codec.encodeResponse(id, result);
                synchronized (conn) {
                    Framing.writeFrame(conn, response);
                }
            }
        } catch (Exception e) {
            String errorType = "Error";
            String message = e.getMessage() != null ? e.getMessage() : e.getClass().getSimpleName();
            byte[] error = Codec.encodeError(id, errorType, message);
            synchronized (conn) {
                Framing.writeFrame(conn, error);
            }
        }
    }

    private static Iterator<Object> queueIterator(BlockingQueue<Object> q) {
        return new Iterator<Object>() {
            private Object next;
            private boolean done = false;
            private boolean fetched = false;

            private void fetch() {
                if (fetched || done) return;
                try {
                    Object v = q.take();
                    if (v == STREAM_INPUT_END) {
                        done = true;
                    } else {
                        next = v;
                    }
                } catch (InterruptedException e) {
                    Thread.currentThread().interrupt();
                    done = true;
                }
                fetched = true;
            }

            @Override
            public boolean hasNext() {
                fetch();
                return !done;
            }

            @Override
            public Object next() {
                fetch();
                if (done) throw new NoSuchElementException();
                fetched = false;
                return next;
            }
        };
    }

    private void runClientStreamHandler(Socket conn, long id, ClientStreamHandler handler, BlockingQueue<Object> q) {
        try {
            Object result = handler.handle(queueIterator(q));
            byte[] response = Codec.encodeResponse(id, result);
            synchronized (conn) {
                Framing.writeFrame(conn, response);
            }
        } catch (Exception e) {
            String message = e.getMessage() != null ? e.getMessage() : e.getClass().getSimpleName();
            try {
                byte[] error = Codec.encodeError(id, "Error", message);
                synchronized (conn) {
                    Framing.writeFrame(conn, error);
                }
            } catch (IOException ignored) {
            }
        }
    }

    private void runBidiHandler(Socket conn, long id, BidiHandler handler, BlockingQueue<Object> q) {
        try {
            Iterator<Object> out = handler.handle(queueIterator(q));
            while (out.hasNext()) {
                Object chunk = out.next();
                byte[] payload = Codec.encodeStreamChunk(id, chunk);
                synchronized (conn) {
                    Framing.writeFrame(conn, payload);
                }
            }
            byte[] end = Codec.encodeStreamEnd(id);
            synchronized (conn) {
                Framing.writeFrame(conn, end);
            }
        } catch (Exception e) {
            String message = e.getMessage() != null ? e.getMessage() : e.getClass().getSimpleName();
            try {
                byte[] error = Codec.encodeError(id, "Error", message);
                synchronized (conn) {
                    Framing.writeFrame(conn, error);
                }
            } catch (IOException ignored) {
            }
        }
    }

    public void close() throws IOException {
        running = false;
        if (serverSocket != null) {
            serverSocket.close();
        }
        threadPool.shutdown();
        streamWorkers.shutdown();
    }
}
