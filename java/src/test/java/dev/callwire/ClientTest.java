package dev.callwire;

import dev.callwire.core.Client;
import dev.callwire.core.Server;
import org.junit.Test;
import org.junit.Before;
import org.junit.After;

import java.util.*;

import static org.junit.Assert.*;

public class ClientTest {

    private Server server;
    private Client client;

    @Before
    public void setUp() throws Exception {
        server = new Server();
        server.export("add", args -> {
            long a = ((Number) args.get(0)).longValue();
            long b = ((Number) args.get(1)).longValue();
            return a + b;
        });
        server.export("greet", args -> "Hello, " + args.get(0) + "!");

        // Start server in background
        new Thread(() -> {
            try {
                server.serve("localhost", 9090);
            } catch (Exception e) {
                e.printStackTrace();
            }
        }).start();

        Thread.sleep(500); // Give server time to start

        client = new Client();
        client.connect("localhost", 9090);
    }

    @After
    public void tearDown() throws Exception {
        if (client != null) {
            client.close();
        }
        if (server != null) {
            server.close();
        }
    }

    @Test
    public void testUnaryCall() throws Exception {
        Object result = client.call("add", Arrays.asList(10L, 20L));
        assertEquals(30L, result);
    }

    @Test
    public void testVarargsCall() throws Exception {
        Object result = client.call("add", 10L, 20L);
        assertEquals(30L, result);
    }

    @Test
    public void testCallLong() throws Exception {
        long result = client.callLong("add", 10L, 20L);
        assertEquals(30L, result);
    }

    @Test
    public void testCallString() throws Exception {
        String result = client.callString("greet", "World");
        assertEquals("Hello, World!", result);
    }

    @Test
    public void testCallLongTypeMismatch() throws Exception {
        try {
            client.callLong("greet", "World");
            fail("Should have thrown an exception for non-numeric result");
        } catch (Exception e) {
            assertTrue(e.getMessage().contains("expected numeric result"));
        }
    }

    @Test
    public void testUnknownFunction() throws Exception {
        try {
            client.call("unknown", new ArrayList<>());
            fail("Should have thrown CallwireException");
        } catch (Exception e) {
            // CallwireException.getMessage() returns only the message text
            // (e.g. "Function 'unknown' not exported") — the error_type
            // ("NotFoundError") is a separate field, not part of getMessage().
            assertTrue(e.getMessage().contains("not exported"));
        }
    }
}
