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

    private final Map<String, Handler> registry = new ConcurrentHashMap<>();
    private ServerSocket serverSocket;
    private volatile boolean running = false;
    private ExecutorService threadPool = Executors.newFixedThreadPool(16);

    public interface Handler {
        Object handle(List<Object> args) throws Exception;
    }

    public interface StreamHandler {
        Iterator<Object> handle(List<Object> args) throws Exception;
    }

    public void export(String funcName, Handler handler) {
        registry.put(funcName, handler);
    }

    public void exportStream(String funcName, StreamHandler handler) {
        registry.put(funcName, (Handler) args -> new StreamMarker(handler.handle(args)));
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
        try {
            while (running) {
                byte[] payload = Framing.readFrame(conn);
                Map<String, Object> msg = Codec.decode(payload);
                dispatch(conn, msg);
            }
        } catch (EOFException e) {
            // Connection closed
        } catch (IOException e) {
            // Log error
        } finally {
            try {
                conn.close();
            } catch (IOException e) {
                // ignore
            }
        }
    }

    private void dispatch(Socket conn, Map<String, Object> msg) throws IOException {
        long id = ((Number) msg.get("id")).longValue();
        String type = (String) msg.get("type");

        if (!"request".equals(type)) {
            return; // ignore non-request messages
        }

        String func = (String) msg.get("func");
        Object argsObj = msg.get("args");
        List<Object> args = argsObj instanceof List ? (List<Object>) argsObj : new ArrayList<>();

        Handler handler = registry.get(func);
        if (handler == null) {
            byte[] error = Codec.encodeError(id, "NotFoundError",
                    "Function '" + func + "' not exported");
            Framing.writeFrame(conn, error);
            return;
        }

        try {
            Object result = handler.handle(args);

            // Check if result is a stream (wrapped in StreamMarker)
            if (result instanceof StreamMarker) {
                Iterator<Object> iter = ((StreamMarker) result).iter;
                while (iter.hasNext()) {
                    Object chunk = iter.next();
                    byte[] payload = Codec.encodeStreamChunk(id, chunk);
                    Framing.writeFrame(conn, payload);
                }
                byte[] end = Codec.encodeStreamEnd(id);
                Framing.writeFrame(conn, end);
            } else {
                // Unary response
                byte[] response = Codec.encodeResponse(id, result);
                Framing.writeFrame(conn, response);
            }
        } catch (Exception e) {
            String errorType = "Error";
            String message = e.getMessage() != null ? e.getMessage() : e.getClass().getSimpleName();
            byte[] error = Codec.encodeError(id, errorType, message);
            Framing.writeFrame(conn, error);
        }
    }

    public void close() throws IOException {
        running = false;
        if (serverSocket != null) {
            serverSocket.close();
        }
        threadPool.shutdown();
    }
}
