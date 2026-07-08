// Java import script: calls "add"(10,20) on every OTHER language's matrix
// export server (best-effort — SKIP if a port isn't reachable).
import dev.callwire.core.Client;
import java.util.LinkedHashMap;
import java.util.Map;

public class MatrixImportJava {
    static void init() {} // no setup needed for a pure client script

    public static void main(String[] args) throws Exception {
        init();

        Map<String, Integer> targets = new LinkedHashMap<>();
        targets.put("go", 9101);
        targets.put("python", 9102);
        targets.put("rust", 9103);
        targets.put("ts", 9104);
        targets.put("c", 9106);
        targets.put("cpp", 9107);
        targets.put("swift", 9108);
        targets.put("cobol", 9109);

        for (Map.Entry<String, Integer> e : targets.entrySet()) {
            String name = e.getKey();
            int port = e.getValue();
            Client client = new Client();
            try {
                client.connect("127.0.0.1", port);
            } catch (Exception ex) {
                System.out.printf("%-8s SKIP (not running: %s)%n", name, ex.getMessage());
                continue;
            }
            try {
                long result = client.callLong("add", 10L, 20L);
                System.out.printf("%-8s OK  add(10,20) = %d%n", name, result);
            } catch (Exception ex) {
                System.out.printf("%-8s SKIP (call failed: %s)%n", name, ex.getMessage());
            } finally {
                client.close();
            }
        }
    }
}
