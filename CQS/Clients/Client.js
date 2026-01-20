require('dotenv').config();

function main() { 
    const url =  process.env.URL_BALANCER ;
    console.log("This is a test client file.", url);
}
main();