#!/usr/bin/awk -f


/ ERROR /   { gsub(/ ERROR /, "\033[1;91m&\033[0m"); }
/code=500/ { gsub(/code=500/, "\033[1;91m&\033[0m"); }
/user_name=\w+? / { gsub(/user_name=\w+ /, "\033[1;92m&\033[0m"); }

/request_id=(\w|+|\\)+?/   { gsub(/request_id=(\w|+|\\)+?/, "\033[1;35m&\033[0m"); }

/listen /   { gsub(/listen /, "\033[1;31m&\033[0m"); }
/ ws /   { gsub(/ws /, "\033[1;31m&\033[0m"); }

{ print; }
