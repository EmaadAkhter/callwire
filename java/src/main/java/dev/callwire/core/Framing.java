package dev.callwire.core;

import java.io.*;
import java.net.Socket;

/**
 * Framing layer: [4-byte big-endian length][N-byte payload]
 */
public class Framing {

    public static byte[] readFrame(Socket socket) throws IOException {
        InputStream in = socket.getInputStream();
        byte[] header = new byte[4];
        int n = in.read(header);
        if (n != 4) {
            throw new EOFException("Failed to read frame header");
        }

        // Parse big-endian uint32
        int len = ((header[0] & 0xFF) << 24)
                | ((header[1] & 0xFF) << 16)
                | ((header[2] & 0xFF) << 8)
                | (header[3] & 0xFF);

        if (len < 0 || len > 16 * 1024 * 1024) {
            throw new IOException("Frame size out of bounds: " + len);
        }

        byte[] payload = new byte[len];
        int readSoFar = 0;
        while (readSoFar < len) {
            int n2 = in.read(payload, readSoFar, len - readSoFar);
            if (n2 <= 0) {
                throw new EOFException("Incomplete frame read");
            }
            readSoFar += n2;
        }

        return payload;
    }

    public static void writeFrame(Socket socket, byte[] payload) throws IOException {
        OutputStream out = socket.getOutputStream();
        int len = payload.length;
        byte[] header = new byte[4];
        header[0] = (byte) ((len >> 24) & 0xFF);
        header[1] = (byte) ((len >> 16) & 0xFF);
        header[2] = (byte) ((len >> 8) & 0xFF);
        header[3] = (byte) (len & 0xFF);
        out.write(header);
        out.write(payload);
        out.flush();
    }
}
