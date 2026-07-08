import dev.callwire.core.Server;
import java.util.*;

/**
 * Java server exporting "add", "greet", and "count_to" functions.
 * Can be called from Go, Python, Rust, TypeScript, or Java clients.
 *
 * Run this server:
 *   cd java && mvn exec:java -Dexec.mainClass=java_server
 *
 * Then call it from another client (e.g., Go):
 *   cd go/callwire && go run examples/client.go
 */
public class java_server {
    public static void main(String[] args) throws Exception {
        Server server = new Server();

        // Export "add" function (unary)
        server.export("add", (List<Object> args) -> {
            long a = ((Number) args.get(0)).longValue();
            long b = ((Number) args.get(1)).longValue();
            return a + b;
        });

        // Export "greet" function (unary)
        server.export("greet", (List<Object> args) -> {
            String name = (String) args.get(0);
            return "Hello, " + name + "!";
        });

        // Export "count_to" function (server-streaming)
        server.exportStream("count_to", (List<Object> args) -> {
            long n = ((Number) args.get(0)).longValue();
            return new Iterator<Object>() {
                private long i = 1;
                public boolean hasNext() { return i <= n; }
                public Object next() { return i++; }
            };
        });

        // Serve on localhost:9090
        server.serve("localhost", 9090);
    }
}
