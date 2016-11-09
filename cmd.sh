#!/usr/bin/bash


# Watches over the directories and executes commands if it
# finds anything.
# 
# It is horrible, i know

CMDMARK="."
CMDDIR="./irc/cmd"


TMPFILE=./irc/queries.tmp

DATELASTCHECK=$(date -r $TMPFILE +"%Y%m%d%H%M%S")

if [ ! -f $TMPFILE ]; then
    touch $TMPFILE
fi

readonly IRCPATH="./irc"
for i in `find $IRCPATH -name 'out'`
do
    grep -v '\-!\-' $i  > /dev/null 2>&1 # if file doesnt just contain server stuff
    if [ $? -ne 1 ]; then
        tail -5 $i | while read line
        do
            timeFile=$(echo $line | cut -d" " -f-2 | xargs -i -0 date -d {} +"%Y%m%d%H%M%S")
            if [ $timeFile -ge $DATELASTCHECK ];
            then
                outFile=$(dirname $i)/in
                cmdLine=$(echo $line|cut -d" " -f4-)
                if [[ $cmdLine == $CMDMARK* ]]; then 
                    cmdFile=$(echo $cmdLine | cut -d"." -f2- | cut -d" " -f1) 
                    cmdArgs=$(echo $cmdLine | cut -d" " -f2-) 
                    $CMDDIR/$cmdFile $cmdArgs > $outFile
                fi
            fi  
        done
    fi
done


touch $TMPFILE
