// Java export script: exports "add" on a fixed port. init() performs setup
// and is called first thing in main().
import dev.callwire.core.Server;
import java.util.List;

public class MatrixExportJava {
    static final int MATRIX_PORT = 9105;
    static Server server;

    static void init() throws Exception {
        server = new Server();
        server.export("add", (List<Object> args) -> {
            long a = ((Number) args.get(0)).longValue();
            long b = ((Number) args.get(1)).longValue();
            return a + b;
        });
    }

    public static void main(String[] args) throws Exception {
        init();
        System.out.println("Java matrix export listening on :" + MATRIX_PORT);
        server.serve("0.0.0.0", MATRIX_PORT);
    }
}
