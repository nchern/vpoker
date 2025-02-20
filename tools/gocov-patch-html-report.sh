#!/bin/sh
set -ue

# HELP: patches go coverage report to add filtering and sorting functionality

patch_html() {
cat << EOF
                <!--- PATCH --->
                <label for="filter_files">Filter: </label>
                <input type="text" id="filter_files" placeholder="Type to filter">
                <label for="sort_files">Sort by coverage: </label>
                <input type="checkbox" id="sort_files" name="sort_files" />
                <!--- END PATCH --->
EOF
}

patch_script() {
    echo "    // PATCH"
    cat << EOF
    (function() {
        function refreshOptions(options) {
            for (let i = 0; i < options.length; i++) {
                var opt = files.options[i];
                opt.selected = false;
                var computedStyle = window.getComputedStyle(opt);
                var displayVal = computedStyle.getPropertyValue('display');
                if (displayVal != "none") {
                    opt.selected = true;
                    break;
                }
            }
        }

        // filtering functionality
        document.getElementById("filter_files").addEventListener("input", function() {
            var val = this.value.toLowerCase();
            var options = document.getElementById("files").getElementsByTagName("option");
            for (var i = 0; i < options.length; i++) {
                var optionText = options[i].text.toLowerCase();
                if (optionText.indexOf(val) > -1) {
                    options[i].style.display = "";
                } else {
                    options[i].style.display = "none";
                }
            }
        });
        document.getElementById("filter_files").addEventListener("keydown", function(e) {
            if (e.key === "Enter") {
                refreshOptions(files.options);
            }
        });

        // sorting functionality
        var percentage = /\([0-9]+?.[0-9]+?%\)/;
        function parseCoverage(opt) {
            var v = opt.text.match(percentage);
            if (v) {
                return parseFloat(v[0].replace(/(\(|\)|%)/g, ""));
            }
            return 0;
        }
        // sort files by coverage
        document.getElementById("sort_files").addEventListener("change", function(e) {
            var cmpFn = function (a, b) { return a.text.localeCompare(b.text); }
            if (e.target.checked) {
                cmpFn = function (a, b) {
                    return parseCoverage(a) - parseCoverage(b);
                }
            }
            var opts = Array.from(files.options);
            opts.sort(cmpFn);
            files.innerHTML = ""; // clear existing options

            opts.forEach(option => { files.appendChild(option); });
            refreshOptions(files.options);
        });
    })();
    // END PATCH
EOF
}


HTML_PATCHED=0
SCRIPT_PATCHED=0

BODY_FINISHED=0
SCRIPT_FINISHED=0

while IFS= read -r line
do
    case "$line" in
        *"<select id=\"files\">" )
            if [ "$HTML_PATCHED" -eq 0 ]; then
                patch_html
                HTML_PATCHED=1
            fi
            ;;
        *"</body>" )
            BODY_FINISHED=1
            ;;
        *"<script>" )
            SCRIPT_FINISHED=0
            ;;
        *"</script>" )
            SCRIPT_FINISHED=1
            ;;
    esac
    if [ "$BODY_FINISHED" -eq 1 ] && [ "$SCRIPT_FINISHED" -eq 1 ] && [ "$SCRIPT_PATCHED" -eq 0 ]; then
        patch_script
        SCRIPT_PATCHED=1
    fi

    echo "$line"
done
