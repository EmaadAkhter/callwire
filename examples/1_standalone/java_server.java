import dev.callwire.core.Server;
import java.util.List;

/**
 * Java server exporting "add" and "greet" functions.
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

        // Export "add" function
        server.export("add", (List<Object> args) -> {
            long a = ((Number) args.get(0)).longValue();
            long b = ((Number) args.get(1)).longValue();
            return a + b;
        });

        // Export "greet" function
        server.export("greet", (List<Object> args) -> {
            String name = (String) args.get(0);
            return "Hello, " + name + "!";
        });

        // Serve on localhost:9090
        server.serve("localhost", 9090);
    }
}
