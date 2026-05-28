export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);

    // Rewrite hostname to OCI Object Storage endpoint
    url.hostname = "objectstorage.eu-amsterdam-1.oraclecloud.com";

    // Prepend OCI bucket path to the request URI path
    url.pathname = `/n/axtkhsokfszw/b/oci-prod-jae-pages-media/o${url.pathname}`;

    // Create a new request object to set custom headers
    const modifiedRequest = new Request(url.toString(), {
      method: request.method,
      headers: request.headers,
      body: request.body,
      redirect: "follow"
    });

    // Set Host header explicitly so OCI accepts the request
    modifiedRequest.headers.set("Host", "objectstorage.eu-amsterdam-1.oraclecloud.com");

    // Fetch from OCI and explicitly tell Cloudflare CDN to cache everything (including video)
    // while ensuring client/server errors are NEVER cached at the Edge.
    const response = await fetch(modifiedRequest, {
      cf: {
        cacheEverything: true,
        cacheTtlByStatus: {
          "200-299": 3600,     // Cache successful downloads for 1 hour at the Edge
          "400-499": 0,        // Do not cache client errors (403, 404) at the Edge
          "500-599": 0         // Do not cache server errors (500, etc.) at the Edge
        }
      }
    });

    // Clone the headers to make them mutable so we can customize caching behavior
    const newHeaders = new Headers(response.headers);
    
    if (response.status === 200 || response.status === 206) {
      // Instruct the user's browser to use the cached copy but revalidate with Cloudflare on every load
      newHeaders.set("Cache-Control", "no-cache, must-revalidate");
    } else {
      // Force browsers to NEVER cache errors, ensuring they fetch fresh when the file is restored
      newHeaders.set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0");
    }

    // Return the response stream with the optimized headers
    return new Response(response.body, {
      status: response.status,
      statusText: response.statusText,
      headers: newHeaders
    });
  }
};
