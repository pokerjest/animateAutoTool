(function() {
    var api;

    htmx.defineExtension("sse", {
        init: function(apiRef) {
            api = apiRef;
        },
        onEvent: function(name, evt) {
            var element = evt.detail.elt;
            if (name === "htmx:beforeCleanupElement") {
                var internalData = api.getInternalData(element);
                if (internalData.sseEventSource) {
                    internalData.sseEventSource.close();
                }
                return;
            }

            if (name === "htmx:afterProcessNode") {
                var sseConnect = element.getAttribute("sse-connect");
                if (sseConnect) {
                    initES(element, sseConnect);
                }
            }
        }
    });

    function initES(element, url) {
        var internalData = api.getInternalData(element);
        if (internalData.sseEventSource) {
            internalData.sseEventSource.close();
        }

        var source = new EventSource(url);
        internalData.sseEventSource = source;

        source.onmessage = function(e) {
            // Log?
        };

        source.onerror = function(e) {
            // Retry logic usually built-in to browser
        };
        
        // Handle swap instructions
        var sseSwap = element.getAttribute("sse-swap");
        if (sseSwap) {
            var events = sseSwap.split(",");
            events.forEach(function(eventName) {
                eventName = eventName.trim();
                source.addEventListener(eventName, function(e) {
                    var content = e.data;
                    // Usually we don't swap here, we just trigger HTMX swap?
                    // Standard ext/sse replaces content of children matching the event name
                    // But for our Progress Bar, we might receive JSON or HTML.
                    // If JSON, we need custom JS. 
                    // Let's implement basic HTML swap support or event triggering.
                    
                    // Trigger a custom event that Alpine can listen to!
                    var customEvent = new CustomEvent("sse:" + eventName, {
                        detail: {
                            type: eventName,
                            data: e.data
                        },
                        bubbles: true
                    });
                    element.dispatchEvent(customEvent);
                    
                    // Also try to swap if ID matches?
                    // The official extension swaps specific children.
                    // For now, dispatching to Alpine is key.
                });
            });
        }
    }
})();
